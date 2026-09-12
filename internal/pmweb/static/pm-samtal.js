const samtalskorningar = new Map();

function samtalshandelseElement(h) {
  const sort = ["text", "verktyg", "fil", "kommando", "fel"].includes(h.sort) ? h.sort : "text";
  const li = document.createElement("li");
  li.className = `handelse h-${sort}`;
  li.innerHTML = `<time>${esc(klocka(h.tid))}</time><span class="handelsesort">${esc(sort)}</span><span>${esc(h.text)}</span>`;
  return li;
}

function byggSamtalsforlopp(post, inlagg) {
  const lage = samtalskorningar.get(post.id);
  if (!lage) return;
  const detaljer = document.createElement("details");
  detaljer.className = "samtalsforlopp";
  detaljer.dataset.samtalskorning = post.id;
  const rubrik = document.createElement("summary");
  rubrik.textContent = lage.klar ? "Agentens arbete" : "Agenten arbetar";
  const lista = document.createElement("ol");
  lista.className = "samtalshandelser";
  lage.handelser.forEach((h) => lista.append(samtalshandelseElement(h)));
  detaljer.append(rubrik, lista);
  inlagg.append(detaljer);
}

function laggSamtalshandelse(inlaggId, event) {
  const lage = samtalskorningar.get(inlaggId);
  if (!lage) return;
  try {
    const handelse = JSON.parse(event.data);
    lage.handelser.push(handelse);
    const lista = document.querySelector(`[data-samtalskorning="${CSS.escape(inlaggId)}"] .samtalshandelser`);
    if (lista) lista.append(samtalshandelseElement(handelse));
  } catch {
    toast("PM kunde inte läsa agentens händelse.");
  }
}

function startaSamtalsstrom(inlaggId, korningId) {
  const lage = { korningId, handelser: [], klar: false };
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
