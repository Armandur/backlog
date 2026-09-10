const alias = decodeURIComponent(location.pathname.replace(/^\/pm\/?/, "").split("/")[0] || "");
const $ = (s) => document.querySelector(s);
const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
let vy = location.hash.replace("#", "") || "projekt";
let valdKorning = null;
let agenter = [];
let konfig = null;
const agentutkast = new Set();
const AGENTMALLAR = {
  tom: { namn: "ny-agent", kommando: "", args: ["{brief}"], brief: "arg", svar: "stdout", stdin: "devnull", timeout_sekunder: 900, miljo: {}, mcp: false },
  claude: { namn: "claude", kommando: "claude", args: ["-p", "{brief}"], brief: "arg", svar: "stdout", stdin: "devnull", timeout_sekunder: 900, miljo: {}, mcp: true },
  codex: { namn: "codex", kommando: "codex", args: ["exec", "-C", "{repo}", "-s", "workspace-write", "-c", "sandbox_workspace_write.network_access=true", "-o", "{svarsfil}", "{brief}"], brief: "arg", svar: "fil", stdin: "devnull", timeout_sekunder: 900, miljo: {}, mcp: false },
};

function tid(ns) {
  return new Date(Number(ns) / 1e6).toLocaleString("sv-SE", { dateStyle: "short", timeStyle: "short" });
}
function klocka(ns) {
  return new Date(Number(ns) / 1e6).toLocaleTimeString("sv-SE", { hour: "2-digit", minute: "2-digit" });
}
function toast(text) {
  const t = $("#toast");
  t.textContent = text;
  t.classList.add("on");
  clearTimeout(t._h);
  t._h = setTimeout(() => t.classList.remove("on"), 3200);
}
// Nyckeln i JSON är ascii, etiketten i vyn är svensk.
const VANTAR_ETIKETT = { fel: "fel", fraga: "fråga", beslut: "beslut" };

// Säg bara att repot är upptaget när en annan körning faktiskt kör.
function koText(k, alla) {
  if (k.status === "kor") return "startade " + klocka(k.startad_at || k.skapad_at);
  const upptaget = alla.some((a) => a.id !== k.id && a.status === "kor" && a.repo_path === k.repo_path);
  return upptaget ? "köad: repot är upptaget av en annan körning" : "köad, startar strax";
}

function pill(status) {
  return `<span class="pill p-${status}">${status}</span>`;
}
function rad(html) {
  const el = document.createElement("div");
  el.className = "rad-post";
  el.innerHTML = html;
  return el;
}
function fyll(id, poster, bygg, tomtext) {
  const ruta = $(id);
  ruta.textContent = "";
  if (!poster.length) {
    const tom = document.createElement("div");
    tom.className = "tom";
    tom.textContent = tomtext;
    ruta.append(tom);
    return;
  }
  poster.forEach((p) => ruta.append(bygg(p)));
}

async function hamta(url, init) {
  const svar = await fetch(url, init);
  const data = await svar.json().catch(() => ({}));
  if (!svar.ok) throw new Error(data.error || `fel från servern (${svar.status})`);
  return data;
}

// ---------- projektvy ----------
async function laddaOversikt() {
  const o = await hamta(`/api/projects/${encodeURIComponent(alias)}/oversikt`);
  $("#projnamn").textContent = `${o.projekt.name} (${o.projekt.alias})`;
  $("#ptitel").textContent = o.projekt.name;
  $("#pdesc").textContent = [o.projekt.description, o.projekt.repo_path && "Repo: " + o.projekt.repo_path].filter(Boolean).join(" ");

  $("#nPagaende").textContent = o.korningar.length;
  $("#nRun").textContent = o.korningar.length || "";
  fyll("#pagaende", o.korningar, (k) => {
    const el = rad(`<span class="mono ref">${k.task_ref}</span>
      <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
        <span class="meta">${koText(k, o.korningar)}</span></div>
      <div class="act">${pill(k.status)}<button class="btn sm" data-logg="${k.id}">Logg</button></div>`);
    el.classList.add("stripe", "kor");
    return el;
  }, "Inga körningar just nu.");

  const oppna = o.tasks.filter((t) => t.status !== "done");
  // Listan visar alla tasks, så säg vad siffran räknar.
  $("#nTasks").textContent = `${oppna.length} öppna av ${o.tasks.length}`;
  fyll("#tasks", o.tasks, (t) => {
    const kor = o.korningar.find((k) => k.task_ref === t.ref);
    const status = kor ? pill(kor.status) : t.status === "done" ? pill("klar") : t.status === "doing" ? pill("kor") : "";
    const knapp = t.status === "done" || kor ? "" : `<button class="btn sm pri" data-dela="${t.ref}">Dela ut</button>`;
    const sista = t.sista_korning ? ` · senaste körning ${t.sista_korning.status}${t.sista_korning.exit_kod !== undefined ? " (exit " + t.sista_korning.exit_kod + ")" : ""}` : "";
    return rad(`<a class="mono ref" href="/tasks/${encodeURIComponent(t.ref)}" title="Öppna i backlog-UI:t">${t.ref}</a>
      <div class="t"><span class="prio">P${t.prioritet}</span> ${esc(t.titel)}
        <span class="meta">${esc(t.typ)}${t.etiketter.length ? " · " + esc(t.etiketter.join(", ")) : ""}${sista}</span></div>
      <div class="act">${status}${knapp}</div>`);
  }, "Inga tasks i projektet.");

  $("#nVantar").textContent = o.vantar.length;
  fyll("#vantar", o.vantar, (v) => {
    const el = rad(`<span class="pill p-${v.sort}">${VANTAR_ETIKETT[v.sort] || esc(v.sort)}</span>
      <div class="t">${v.ref ? `<span class="mono">${v.ref}</span>` : ""}<span class="meta">${esc(v.text)}</span></div>
      <div class="act">${v.sort === "fraga" ? `<button class="btn sm" data-vy="samtal">Öppna</button>` : v.korning_id ? `<button class="btn sm" data-logg="${v.korning_id}">Logg</button>` : `<button class="btn sm pri" data-dela="${v.ref}">Dela ut</button>`}</div>`);
    el.classList.add("stripe", v.sort);
    return el;
  }, "Inget väntar på dig.");

  $("#nBlockerat").textContent = o.blockerat.length;
  fyll("#blockerat", o.blockerat, (t) => rad(`<a class="mono ref" href="/tasks/${encodeURIComponent(t.ref)}">${t.ref}</a>
    <div class="t">${esc(t.titel)}<span class="meta">${esc(t.skal)}</span></div><div class="act"></div>`), "Inget blockerat.");

  const textrader = Array.from(document.querySelectorAll(".rad-post .t"));
  textrader.forEach((el) => el.setAttribute("title", el.textContent.trim()));
}

// ---------- körningar ----------
async function laddaKorningar() {
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/korningar`);
  fyll("#korningar", data.korningar, (k) => rad(`<span class="mono ref">${k.task_ref}</span>
    <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
      <span class="meta">${tid(k.skapad_at)}${k.exit_kod !== undefined ? " · exit " + k.exit_kod : ""}</span></div>
    <div class="act">${pill(k.status)}<button class="btn sm" data-logg="${k.id}">Logg</button></div>`), "Inga körningar än.");
}

async function visaLogg(id) {
  valdKorning = id;
  const data = await hamta(`/api/korningar/${encodeURIComponent(id)}?logg=1`);
  $("#loggruta").hidden = false;
  $("#loggtitel").textContent = `${data.korning.task_ref} · ${data.korning.agent} · ${data.korning.status}`;
  $("#logg").textContent = data.logg || "(ingen logg skriven än)";
  byt("korningar", true);
  $("#loggruta").scrollIntoView({ block: "nearest" });
}

// ---------- samtal ----------
async function laddaSamtal() {
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/samtal`);
  fyll("#trad", data.samtal || [], (p) => {
    const li = document.createElement("li");
    li.className = "inlagg" + (p.actor.kind === "ai" ? " ai" : "");
    const meta = document.createElement("div");
    meta.className = "meta";
    const aktor = document.createElement("span");
    aktor.className = "aktor";
    aktor.textContent = `${p.actor.kind}:${p.actor.name}`;
    const nar = document.createElement("span");
    nar.textContent = tid(p.created_at);
    meta.append(aktor, nar);
    const text = document.createElement("p");
    text.className = "text";
    text.textContent = p.text;
    li.append(meta, text);
    return li;
  }, "Tråden är tom. Skriv det första inlägget.");
}

$("#skrivform").addEventListener("submit", async (e) => {
  e.preventDefault();
  const text = $("#text").value.trim();
  if (!text) return;
  const knapp = $("#skicka");
  knapp.disabled = true;
  const fragar = $("#fraga").checked;
  toast(fragar ? "Frågar agenten..." : "Sparar...");
  try {
    await hamta(`/api/projects/${encodeURIComponent(alias)}/samtal`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, actor: $("#aktor").value.trim(), fraga: fragar }),
    });
    $("#text").value = "";
    await laddaSamtal();
  } catch (err) {
    toast(err.message);
  } finally {
    knapp.disabled = false;
  }
});

// ---------- utdelning ----------
let delaRef = null;
function oppnaDela(ref) {
  delaRef = ref;
  $("#dTask").textContent = ref;
  $("#dAgent").innerHTML = `<option value="">regelvald agent</option>` + agenter.map((a) => `<option value="${esc(a)}">${esc(a)}</option>`).join("");
  $("#dNotis").hidden = true;
  $("#drawer").classList.add("on");
  $("#scrim").classList.add("on");
}
function stangDela() {
  $("#drawer").classList.remove("on");
  $("#scrim").classList.remove("on");
}
$("#stang").onclick = $("#dAvbryt").onclick = stangDela;
$("#scrim").onclick = stangDela;
$("#dStarta").onclick = async () => {
  const knapp = $("#dStarta");
  knapp.disabled = true;
  try {
    await hamta(`/api/projects/${encodeURIComponent(alias)}/dela-ut`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ task: delaRef, agent: $("#dAgent").value }),
    });
    stangDela();
    toast(`${delaRef} utdelad. Följ körningen under pågående.`);
    await ladda();
  } catch (err) {
    $("#dNotis").hidden = false;
    $("#dNotis").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
};

// ---------- skal ----------
function byt(ny, behallScroll) {
  const bytteVy = vy !== ny;
  vy = ny;
  // replaceState i stället för location.hash: annars hoppar webbläsaren till
  // elementet med samma id, t.ex. listan #korningar.
  history.replaceState(null, "", "#" + ny);
  document.querySelectorAll(".rail button").forEach((b) => b.classList.toggle("on", b.dataset.v === vy));
  document.querySelectorAll(".view").forEach((v) => v.classList.toggle("on", v.id === "v-" + vy));
  // En ny vy börjar på sin egen topp, inte där förra vyn var skrollad.
  if (bytteVy && !behallScroll) window.scrollTo(0, 0);
  if (ny === "konfig") laddaKonfig();
}
$("#nav").addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (b) byt(b.dataset.v);
});
document.body.addEventListener("click", (e) => {
  const d = e.target.closest("[data-dela]");
  if (d) return oppnaDela(d.dataset.dela);
  const l = e.target.closest("[data-logg]");
  if (l) return visaLogg(l.dataset.logg);
  const v = e.target.closest("[data-vy]");
  if (v) return byt(v.dataset.vy);
});

async function laddaAgenter() {
  try {
    const data = await hamta("/api/agenter");
    agenter = data.agenter || [];
  } catch {
    agenter = [];
  }
}

async function ladda() {
  if (!alias) {
    $("#projnamn").textContent = "Konfiguration";
    if (vy !== "konfig") byt("konfig");
    return;
  }
  try {
    await Promise.all([laddaOversikt(), laddaKorningar(), laddaSamtal()]);
  } catch (err) {
    toast(err.message);
  }
}

byt(vy);
laddaAgenter().then(ladda);
// Polling håller pågående körningar aktuella utan omladdning.
setInterval(ladda, 4000);

// ---------- konfiguration ----------
function rader(varden) { return (varden || []).join("\n"); }
function kommaseparerat(varden) { return (varden || []).join(", "); }
function miljoRader(miljo) { return Object.entries(miljo || {}).map(([namn, varde]) => `${namn}=${varde}`).join("\n"); }
function lasMiljo(text, falt) {
  const miljo = {};
  text.split("\n").map((rad) => rad.trim()).filter(Boolean).forEach((rad) => {
    const delning = rad.indexOf("=");
    if (delning < 1) throw new Error(`${falt}: använd NYCKEL=värde, en per rad.`);
    miljo[rad.slice(0, delning).trim()] = rad.slice(delning + 1);
  });
  return miljo;
}
function lasLista(text, separator) {
  return text.split(separator).map((v) => v.trim()).filter(Boolean);
}
function agentAlternativ(vald) {
  const namn = Object.keys(konfig.agenter || {}).sort();
  return namn.map((n) => `<option value="${esc(n)}"${n === vald ? " selected" : ""}>${esc(n)}</option>`).join("");
}

function normaliseraKonfig(data) {
  data.agenter ||= {}; data.regler ||= []; data.krok ||= {}; data.krok.anspraka ||= []; data.krok.slapp ||= []; data.krok.miljo ||= {};
  return data;
}

function renderaKonfig() {
  if (!konfig) return;
  const kalla = konfig.saknas ? `Filen ${konfig.sokvag} saknas. Inbyggda defaulter visas.` : `Läser ${konfig.sokvag}.`;
  $("#konfigMeta").textContent = kalla;
  $("#konfigFel").textContent = "";

  const namn = Object.keys(konfig.agenter || {}).sort();
  $("#defaultAgent").innerHTML = namn.map((n) => `<option value="${esc(n)}"${n === konfig.default_agent ? " selected" : ""}>${esc(n)}</option>`).join("");
  $("#agentkort").innerHTML = namn.map((n) => {
    const a = konfig.agenter[n];
    return `<article class="konfigkort agentkort" data-agent="${esc(n)}">
      <div class="korthuvud">
        <label class="falt agentnamn">Namn<input data-agentfalt="namn" value="${esc(n)}" spellcheck="false"></label>
        <div class="kortknappar"><button type="button" class="btn sm" data-prova-agent>Prova</button><button type="button" class="btn sm fara" data-ta-bort-agent>Ta bort</button></div>
      </div>
      <div class="faltgrid agentgrid">
        <label class="falt">Kommando<input data-agentfalt="kommando" value="${esc(a.kommando || "")}" spellcheck="false"></label>
        <label class="falt">Timeout i sekunder<input data-agentfalt="timeout" type="number" min="1" value="${Number(a.timeout_sekunder) || 900}"></label>
        <label class="falt">Brief-läge<select data-agentfalt="brief"><option value="arg"${a.brief === "arg" ? " selected" : ""}>arg</option><option value="stdin"${a.brief === "stdin" ? " selected" : ""}>stdin</option></select></label>
        <label class="falt">Svar-läge<select data-agentfalt="svar"><option value="stdout"${a.svar === "stdout" ? " selected" : ""}>stdout</option><option value="fil"${a.svar === "fil" ? " selected" : ""}>fil</option></select></label>
      </div>
      <label class="falt">Args, ett argument per rad<textarea data-agentfalt="args" rows="4" spellcheck="false">${esc(rader(a.args))}</textarea></label>
      <details><summary>Fler inställningar</summary>
        <div class="faltgrid">
          <label class="falt">Stdin<input data-agentfalt="stdin" value="${esc(a.stdin || "")}" spellcheck="false"></label>
          <label class="kryss detaljkryss"><input data-agentfalt="mcp" type="checkbox"${a.mcp ? " checked" : ""}> Lägg till MCP-konfiguration</label>
        </div>
        <label class="falt"><span>Miljö, en <code>NYCKEL=värde</code> per rad</span><textarea data-agentfalt="miljo" rows="3" spellcheck="false">${esc(miljoRader(a.miljo))}</textarea></label>
      </details>
      <pre class="provsvar" hidden></pre>
    </article>`;
  }).join("") || '<div class="tom">Inga agenter. Lägg till en innan du sparar.</div>';
  document.querySelectorAll(".agentkort").forEach((kort) => {
    $("#agentHjalp").content.querySelectorAll("[data-hjalp]").forEach((hjalp) => {
      kort.querySelector('[data-agentfalt="' + hjalp.dataset.hjalp + '"]').closest("label").append(hjalp.cloneNode(true));
    });
    if (agentutkast.has(kort.dataset.agent)) {
      kort.classList.add("utkast"); kort.prepend($("#utkastMarke").content.cloneNode(true));
    }
  });

  $("#regelkort").innerHTML = (konfig.regler || []).map((regel, i) => `<article class="konfigkort regelkort" data-regel="${i}">
    <div class="korthuvud"><strong>Regel ${i + 1}</strong><div class="kortknappar">
      <button type="button" class="btn sm" data-flytta-regel="-1" aria-label="Flytta upp"${i === 0 ? " disabled" : ""}>↑</button>
      <button type="button" class="btn sm" data-flytta-regel="1" aria-label="Flytta ned"${i === konfig.regler.length - 1 ? " disabled" : ""}>↓</button>
      <button type="button" class="btn sm fara" data-ta-bort-regel>Ta bort</button>
    </div></div>
    <div class="faltgrid regelgrid">
      <label class="falt">Namn<input data-regelfalt="namn" value="${esc(regel.namn || "")}"></label>
      <label class="falt">Agent<select data-regelfalt="agent">${agentAlternativ(regel.agent)}</select></label>
      <label class="falt">Typer, kommaseparerade<input data-regelfalt="typ" value="${esc(kommaseparerat(regel.typ))}"></label>
      <label class="falt">Etiketter, kommaseparerade<input data-regelfalt="etiketter" value="${esc(kommaseparerat(regel.etiketter))}"></label>
    </div>
    <label class="falt">Nyckelord, kommaseparerade<input data-regelfalt="nyckelord" value="${esc(kommaseparerat(regel.nyckelord))}"></label>
  </article>`).join("") || '<div class="tom">Inga regler. Den förvalda agenten används.</div>';

  $("#krokAnspraka").value = rader(konfig.krok?.anspraka);
  $("#krokSlapp").value = rader(konfig.krok?.slapp);
  $("#krokMiljo").value = miljoRader(konfig.krok?.miljo);
}

function samlaKonfig() {
  const agenterNy = {};
  const namnbyten = {};
  document.querySelectorAll(".agentkort").forEach((kort) => {
    const hamta = (falt) => kort.querySelector(`[data-agentfalt="${falt}"]`);
    const namn = hamta("namn").value.trim();
    if (!namn) throw new Error("Alla agenter måste ha ett namn.");
    if (agenterNy[namn]) throw new Error(`Agentnamnet ${namn} används mer än en gång.`);
    namnbyten[kort.dataset.agent] = namn;
    agenterNy[namn] = {
      kommando: hamta("kommando").value.trim(),
      args: lasLista(hamta("args").value, "\n"),
      brief: hamta("brief").value,
      svar: hamta("svar").value,
      stdin: hamta("stdin").value.trim(),
      timeout_sekunder: Number(hamta("timeout").value),
      miljo: lasMiljo(hamta("miljo").value, `Miljön för ${namn}`),
      mcp: hamta("mcp").checked,
    };
  });
  const reglerNy = Array.from(document.querySelectorAll(".regelkort")).map((kort) => {
    const hamta = (falt) => kort.querySelector(`[data-regelfalt="${falt}"]`).value;
    return {
      namn: hamta("namn").trim(), agent: namnbyten[hamta("agent")] || hamta("agent"),
      typ: lasLista(hamta("typ"), ","), etiketter: lasLista(hamta("etiketter"), ","),
      nyckelord: lasLista(hamta("nyckelord"), ","),
    };
  });
  const forval = namnbyten[$("#defaultAgent").value] || $("#defaultAgent").value;
  return {
    // Metadatan följer med utkastet, annars tappar metaraden sökvägen.
    sokvag: konfig?.sokvag,
    saknas: konfig?.saknas,
    default_agent: forval, agenter: agenterNy, regler: reglerNy,
    krok: {
      anspraka: lasLista($("#krokAnspraka").value, "\n"),
      slapp: lasLista($("#krokSlapp").value, "\n"),
      miljo: lasMiljo($("#krokMiljo").value, "Krokens miljö"),
    },
  };
}

async function laddaKonfig() {
  try {
    konfig = normaliseraKonfig(await hamta("/api/konfig"));
    renderaKonfig();
  } catch (err) {
    $("#konfigFel").textContent = err.message;
  }
}

$("#forslagsform").addEventListener("submit", async (e) => {
  e.preventDefault();
  const knapp = $("#hamtaForslag");
  $("#forslagsFel").textContent = "";
  knapp.disabled = true;
  $("#forslagsStatus").hidden = false;
  try {
    konfig = samlaKonfig();
    const forslag = await hamta("/api/konfig/foresla", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ beskrivning: $("#verktygsbeskrivning").value }),
    });
    const { namn: basnamn, ...agent } = forslag;
    let namn = basnamn, nummer = 2;
    while (konfig.agenter[namn]) namn = basnamn + "-" + nummer++;
    konfig.agenter[namn] = agent; agentutkast.add(namn);
    if (!konfig.default_agent) konfig.default_agent = namn;
    renderaKonfig();
  } catch (err) { $("#forslagsFel").textContent = err.message; } finally { knapp.disabled = false; $("#forslagsStatus").hidden = true; }
});
$("#laggTillAgent").onclick = () => {
  try { konfig = samlaKonfig(); } catch (err) { return toast(err.message); }
  const mall = AGENTMALLAR[$("#agentmall").value] || AGENTMALLAR.tom;
  const { namn: basnamn, ...agent } = mall;
  let namn = basnamn;
  let nummer = 2;
  while (konfig.agenter[namn]) namn = basnamn + "-" + nummer++;
  konfig.agenter[namn] = { ...agent, args: [...agent.args], miljo: {} };
  if (!konfig.default_agent) konfig.default_agent = namn;
  renderaKonfig();
};
$("#laggTillRegel").onclick = () => {
  try { konfig = samlaKonfig(); } catch (err) { return toast(err.message); }
  konfig.regler.push({ namn: "", typ: [], etiketter: [], nyckelord: [], agent: konfig.default_agent });
  renderaKonfig();
};
$("#v-konfig").addEventListener("click", async (e) => {
  const agentkort = e.target.closest(".agentkort");
  const regelkort = e.target.closest(".regelkort");
  if (e.target.closest("[data-ta-bort-agent]")) {
    try { konfig = samlaKonfig(); } catch (err) { return toast(err.message); }
    delete konfig.agenter[agentkort.querySelector('[data-agentfalt="namn"]').value.trim()];
    if (!konfig.agenter[konfig.default_agent]) konfig.default_agent = Object.keys(konfig.agenter).sort()[0] || "";
    renderaKonfig();
    return;
  }
  if (e.target.closest("[data-ta-bort-regel]")) {
    try { konfig = samlaKonfig(); } catch (err) { return toast(err.message); }
    konfig.regler.splice(Number(regelkort.dataset.regel), 1);
    renderaKonfig();
    return;
  }
  const flytta = e.target.closest("[data-flytta-regel]");
  if (flytta) {
    try { konfig = samlaKonfig(); } catch (err) { return toast(err.message); }
    const fran = Number(regelkort.dataset.regel);
    const till = fran + Number(flytta.dataset.flyttaRegel);
    [konfig.regler[fran], konfig.regler[till]] = [konfig.regler[till], konfig.regler[fran]];
    renderaKonfig();
    return;
  }
  const prova = e.target.closest("[data-prova-agent]");
  if (prova) {
    const namn = agentkort.querySelector('[data-agentfalt="namn"]').value.trim();
    const ruta = agentkort.querySelector(".provsvar");
    prova.disabled = true;
    ruta.hidden = false;
    ruta.textContent = "Kör testbriefen...";
    try {
      const resultat = await hamta("/api/konfig/prova", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ agent: namn }) });
      ruta.textContent = `Exitkod ${resultat.exitkod}\n${resultat.svar || "(tomt svar)"}`;
    } catch (err) {
      ruta.textContent = err.message + "\nSpara agenten före provet om du nyss ändrade den.";
    } finally {
      prova.disabled = false;
    }
  }
});
$("#konfigform").addEventListener("submit", async (e) => {
  e.preventDefault();
  const knapp = $("#sparaKonfig");
  knapp.disabled = true;
  try {
    const utkast = samlaKonfig();
    konfig = utkast;
    konfig = normaliseraKonfig(await hamta("/api/konfig", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(utkast) }));
    agenter = Object.keys(konfig.agenter || {}).sort();
    agentutkast.clear();
    renderaKonfig();
    toast("pm.toml är sparad.");
  } catch (err) {
    $("#konfigFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
});
