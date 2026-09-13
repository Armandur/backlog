const samtalskorningar = new Map();
// Tråden ritas om vid varje pollning. Utan minne av vilka rutor användaren
// fällt ut skulle de slå ihop sig var fjärde sekund.
const utfalldaForlopp = new Set();
const saknadeHandelsefiler = new Set();

// Verben skrivs av PM i internal/pm/handelse.go, funktionerna kring raderna
// 210 och 403. Listan måste spegla dem, annars länkas inte filen. Ett Go-prov
// i handelsefil_test.go vaktar att de två listorna är lika.
const FILVERB = ["ändrade", "skapade", "tog bort", "hanterade", "läste", "sökte i"];
// En borttagen fil finns inte kvar att öppna. Den visas utan länk direkt,
// i stället för att ge ett fel när någon klickar.
const BORTTAGET_VERB = "tog bort";

function filFranHandelse(h) {
  if (h.sort !== "fil") return null;
  const text = String(h.text || "").trim();
  const verb = FILVERB.find((v) => text.toLowerCase().startsWith(v + " "));
  const sokvag = relativSokvag((verb ? text.slice(verb.length) : text).trim());
  if (!sokvag) return null;
  return {
    inledning: verb ? verb + " " : "",
    sokvag: sokvag.text,
    borttagen: verb === BORTTAGET_VERB,
    utanfor: sokvag.utanfor,
  };
}

// Agenterna skriver absoluta sökvägar, filvyn tar relativa mot repot. En fil
// utanför repot kan filvyn inte visa, då står sökvägen kvar som text.
function relativSokvag(sokvag) {
  if (!sokvag) return null;
  if (!sokvag.startsWith("/")) return { text: sokvag, utanfor: false };
  const rot = projektRepo.replace(/\/+$/, "");
  if (rot && sokvag.startsWith(rot + "/")) return { text: sokvag.slice(rot.length + 1), utanfor: false };
  return { text: sokvag, utanfor: true };
}

function filhandelseinnehall(fil) {
  const innehall = document.createElement("span");
  if (fil.inledning) innehall.append(document.createTextNode(fil.inledning));
  if (fil.utanfor) {
    const sokvag = document.createElement("span");
    sokvag.className = "handelsefil";
    sokvag.textContent = fil.sokvag;
    innehall.append(sokvag);
    return innehall;
  }
  if (fil.borttagen || saknadeHandelsefiler.has(fil.sokvag)) {
    const sokvag = document.createElement("span");
    sokvag.className = "handelsefil saknas";
    sokvag.textContent = fil.sokvag;
    const besked = document.createElement("small");
    besked.className = "filbesked";
    besked.textContent = "Filen finns inte längre.";
    innehall.append(sokvag, besked);
    return innehall;
  }
  const lank = document.createElement("a");
  lank.className = "handelsefil";
  lank.dataset.handelsefil = fil.sokvag;
  lank.href = `#filer?fil=${encodeURIComponent(fil.sokvag)}`;
  lank.textContent = fil.sokvag;
  innehall.append(lank);
  return innehall;
}

function markeraSaknadHandelsefil(sokvag) {
  saknadeHandelsefiler.add(sokvag);
  document.querySelectorAll("a[data-handelsefil]").forEach((lank) => {
    if (lank.dataset.handelsefil !== sokvag) return;
    const ersattning = filhandelseinnehall({ inledning: "", sokvag });
    lank.replaceWith(...ersattning.childNodes);
  });
}

function samtalshandelseElement(h, seddaFiler) {
  const sort = ["text", "verktyg", "fil", "kommando", "fel"].includes(h.sort) ? h.sort : "text";
  const fil = filFranHandelse(h);
  if (fil && seddaFiler && seddaFiler.has(fil.sokvag)) return null;
  if (fil && seddaFiler) seddaFiler.add(fil.sokvag);
  const li = document.createElement("li");
  li.className = `handelse h-${sort}`;
  const tidpunkt = document.createElement("time");
  tidpunkt.textContent = klocka(h.tid);
  const etikett = document.createElement("span");
  etikett.className = "handelsesort";
  etikett.textContent = sort;
  const innehall = fil ? filhandelseinnehall(fil) : document.createElement("span");
  if (!fil) innehall.textContent = h.text;
  li.append(tidpunkt, etikett, innehall);
  return li;
}

function tokentext(antal) {
  if (!antal) return "";
  if (antal < 1000) return `${antal} tokens`;
  return `${(antal / 1000).toFixed(1)}k tokens`;
}

function sekundtext(sekunder) {
  const hela = Math.max(0, Math.round(sekunder));
  if (hela < 60) return `${hela} s`;
  return `${Math.floor(hela / 60)} min ${String(hela % 60).padStart(2, "0")} s`;
}

function tidtext(start) {
  return sekundtext((Date.now() - start) / 1000);
}

// ritaArbetsrad uppdaterar bara statusraden, aldrig hela tråden. En timer som
// ritar om tråden varje sekund skulle kasta bort skrollposition och utfällning.
function ritaArbetsrad(inlaggId, element) {
  const lage = samtalskorningar.get(inlaggId);
  // Elementet skickas med när kortet byggs, för då ligger det ännu i ett
  // fragment och går inte att hitta i dokumentet.
  const kort = element || document.querySelector(`[data-arbetskort="${CSS.escape(inlaggId)}"]`);
  if (!lage || !kort) return;
  const status = kort.querySelector(".arbetsstatus");
  const senaste = lage.senaste && lage.senaste.text ? lage.senaste.text.split("\n")[0] : "";
  // Ett avslutat kort säger bara tid och tokens. Att skriva agentens arbete
  // säger inget som inte redan syns.
  status.textContent = lage.klar ? "" : senaste || "Agenten tänker";
  kort.classList.toggle("arbetar", !lage.klar);
  const delar = [];
  if (lage.start) delar.push(tidtext(lage.start));
  else if (lage.varaktighet) delar.push(sekundtext(lage.varaktighet));
  const tokens = tokentext(lage.tokens);
  if (tokens) delar.push(tokens);
  kort.querySelector(".arbetsmeta").textContent = delar.join(" · ");
}

// byggArbetsinlagg lägger agentens arbete som ett eget inlägg på agentens
// sida, så tråden läses som ett samtal och inte som en bilaga till frågan.
// Kortet hör till svaret, inte till frågan. Medan agenten arbetar finns inget
// svar ännu, och då hänger kortet på frågan tills svaret kommer.
function byggSamtalsforlopp(post, lista) {
  let lage = samtalskorningar.get(post.id);
  if (lage && lage.klar && !lage.historisk) {
    // Svaret har landat och bär nu körningen. Frågans kort ska bort.
    samtalskorningar.delete(post.id);
    lage = null;
  }
  if (!lage && post.korning_id) {
    // Ett avslutat svar har sin körning i databasen. Stegen hämtas först när
    // användaren fäller ut rutan, annars läser PM filer ingen tittar på.
    lage = { korningId: post.korning_id, handelser: [], klar: true, historisk: true,
             tokens: post.korning_tokens || 0, varaktighet: post.korning_sekunder || 0 };
    samtalskorningar.set(post.id, lage);
  }
  if (!lage) return;
  const li = document.createElement("li");
  li.className = "arbetskort" + (lage.klar ? " avslutad" : " arbetar");
  li.dataset.arbetskort = post.id;

  const rad = document.createElement("div");
  rad.className = "arbetsrad";
  const punkt = document.createElement("span");
  punkt.className = "arbetspuls";
  punkt.setAttribute("aria-hidden", "true");
  const status = document.createElement("span");
  status.className = "arbetsstatus";
  const meta = document.createElement("span");
  meta.className = "arbetsmeta";
  rad.append(punkt, status, meta);

  const detaljer = document.createElement("details");
  detaljer.className = "samtalsforlopp";
  detaljer.dataset.samtalskorning = post.id;
  detaljer.open = utfalldaForlopp.has(post.id);
  detaljer.addEventListener("toggle", () => {
    if (detaljer.open) {
      utfalldaForlopp.add(post.id);
      hamtaHistoriskaSteg(post.id, handelselista);
    } else {
      utfalldaForlopp.delete(post.id);
    }
  });
  const rubrik = document.createElement("summary");
  rubrik.textContent = "Visa stegen";
  const handelselista = document.createElement("ol");
  handelselista.className = "samtalshandelser";
  lage.filer = new Set();
  lage.handelser.forEach((h) => {
    const element = samtalshandelseElement(h, lage.filer);
    if (element) handelselista.append(element);
  });
  detaljer.append(rubrik, handelselista);

  li.append(rad, detaljer);
  lista.append(li);
  ritaArbetsrad(post.id, li);
}

// hamtaHistoriskaSteg läser stegen för en avslutad körning, en gång.
async function hamtaHistoriskaSteg(inlaggId, lista) {
  const lage = samtalskorningar.get(inlaggId);
  if (!lage || !lage.historisk || lage.hamtad) return;
  lage.hamtad = true;
  try {
    const data = await hamta(`/api/korningar/${encodeURIComponent(lage.korningId)}/handelser`);
    const alla = data.handelser || [];
    lage.handelser = alla.filter((h) => h.sort !== "tokens" && h.sort !== "tokens_total");
    // Slutsumman gäller när den finns, annars summan av stegen.
    const total = alla.filter((h) => h.sort === "tokens_total").pop();
    lage.tokens = total
      ? Number(total.text) || 0
      : alla.filter((h) => h.sort === "tokens").reduce((summa, h) => summa + (Number(h.text) || 0), 0);
    if (data.startad_at && data.slut_at) lage.varaktighet = (data.slut_at - data.startad_at) / 1e9;
    lista.replaceChildren();
    lage.filer = new Set();
    lage.handelser.forEach((h) => {
      const element = samtalshandelseElement(h, lage.filer);
      if (element) lista.append(element);
    });
    ritaArbetsrad(inlaggId, lista.closest(".arbetskort"));
  } catch (err) {
    lage.hamtad = false;
    toast(err.message);
  }
}

// Timern tickar i sin egen takt och rör bara statusraderna.
setInterval(() => {
  samtalskorningar.forEach((lage, inlaggId) => {
    if (!lage.klar) ritaArbetsrad(inlaggId);
  });
}, 1000);

function laggSamtalshandelse(inlaggId, event) {
  const lage = samtalskorningar.get(inlaggId);
  if (!lage) return;
  try {
    const handelse = JSON.parse(event.data);
    if (handelse.sort === "tokens" || handelse.sort === "tokens_total") {
      // Stegen adderas löpande. Slutsumman från agenten ersätter dem, annars
      // räknas varje steg två gånger.
      const antal = Number(handelse.text) || 0;
      lage.tokens = handelse.sort === "tokens_total" ? antal : lage.tokens + antal;
      ritaArbetsrad(inlaggId);
      return;
    }
    lage.handelser.push(handelse);
    lage.senaste = handelse;
    ritaArbetsrad(inlaggId);
    const lista = document.querySelector(`[data-samtalskorning="${CSS.escape(inlaggId)}"] .samtalshandelser`);
    if (!lista) return;
    // Följ med i agentens arbete, men bara för den som redan står längst ned.
    // Läsningen måste ske före tillägget, annars har rutan redan vuxit.
    const trad = $("#trad");
    const foljMed = vidBotten(trad);
    const element = samtalshandelseElement(handelse, lage.filer || (lage.filer = new Set()));
    if (element) lista.append(element);
    if (element && foljMed) skrollaNed(trad);
  } catch {
    toast("PM kunde inte läsa agentens händelse.");
  }
}

function startaSamtalsstrom(inlaggId, korningId) {
  const lage = { korningId, handelser: [], klar: false, start: Date.now(), tokens: 0 };
  samtalskorningar.set(inlaggId, lage);
  const kallan = new EventSource(`/api/korningar/${encodeURIComponent(korningId)}/strom`);
  lage.kallan = kallan;
  kallan.onmessage = (event) => laggSamtalshandelse(inlaggId, event);
  kallan.addEventListener("slut", async (event) => {
    laggSamtalshandelse(inlaggId, event);
    lage.klar = true;
    kallan.close();
    await laddaSamtal();
    await laddaOversikt();
  });
  kallan.onerror = () => {
    kallan.close();
    lage.klar = true;
    lage.handelser.push({ tid: Date.now() * 1e6, sort: "fel", text: "Anslutningen till agenten bröts." });
    laddaSamtal().catch((err) => toast(err.message));
  };
}

function byggMinnesforslag(post, inlagg) {
  PMTal.byggUpplasning(post, inlagg);
  if (!post.minnesforslag) return;
  const ruta = document.createElement("div");
  ruta.className = "minnesforslag";
  const etikett = document.createElement("label");
  etikett.textContent = "Förslag till projektminnet";
  const text = document.createElement("textarea");
  text.rows = 3;
  text.value = post.minnesforslag;
  text.dataset.minnestext = post.id;
  etikett.htmlFor = `minne-${post.id}`;
  text.id = etikett.htmlFor;
  const rad = document.createElement("div");
  rad.className = "rad";
  const tips = document.createElement("span");
  tips.className = "meta";
  tips.textContent = "Redigera texten innan du sparar den.";
  const knapp = document.createElement("button");
  knapp.type = "button";
  knapp.className = "btn sm pri";
  knapp.dataset.sparaMinne = post.id;
  knapp.textContent = "Spara i minnet";
  rad.append(tips, knapp);
  ruta.append(etikett, text, rad);
  inlagg.append(ruta);
}

async function sparaMinnesforslag(knapp) {
  const id = knapp.dataset.sparaMinne;
  const text = document.querySelector(`[data-minnestext="${CSS.escape(id)}"]`).value.trim();
  if (!text) {
    toast("Minnesförslaget saknar text.");
    return;
  }
  knapp.disabled = true;
  try {
    await hamta(`/api/projects/${encodeURIComponent(alias)}/samtal/${encodeURIComponent(id)}/minne`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text }),
    });
    toast("PM sparade minnesposten.");
    await laddaSamtal();
  } catch (err) {
    toast(err.message);
    knapp.disabled = false;
  }
}

async function kvitteraAgentsvar(id) {
  await hamta(`/api/projects/${encodeURIComponent(alias)}/samtal/${encodeURIComponent(id)}/kvittera`, { method: "POST" });
  toast("PM kvitterade agentsvaret.");
  await laddaOversikt();
}

document.body.addEventListener("click", (event) => {
  const minne = event.target.closest("[data-spara-minne]");
  if (minne) return sparaMinnesforslag(minne);
  const kvittens = event.target.closest("[data-kvittera]");
  if (kvittens) kvitteraAgentsvar(kvittens.dataset.kvittera).catch((err) => toast(err.message));
});

const samtalsvy = document.querySelector("#v-samtal");
if (PMTal.harTalstod && samtalsvy) {
  new MutationObserver(() => {
    if (!samtalsvy.classList.contains("on")) PMTal.stoppaUpplasning();
  }).observe(samtalsvy, { attributes: true, attributeFilter: ["class"] });
  window.addEventListener("pagehide", PMTal.stoppaUpplasning);
}
