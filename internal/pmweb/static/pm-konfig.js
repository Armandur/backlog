// Konfigvyn: agenter, regler och krok. Laddas efter pm.js.
let konfig = null;
let konfigProjekt = [];
const agentutkast = new Set();
const raderadeTestservrar = new Set();
const AGENTMALLAR = {
  tom: { namn: "ny-agent", kommando: "", args: ["{brief}"], brief: "arg", svar: "stdout", stdin: "devnull", timeout_sekunder: 900, miljo: {}, mcp: false },
  claude: { namn: "claude", kommando: "claude", args: ["-p", "{brief}"], brief: "arg", svar: "stdout", stdin: "devnull", timeout_sekunder: 900, miljo: {}, mcp: true, strom: "claude-json" },
  codex: { namn: "codex", kommando: "codex", args: ["exec", "-C", "{repo}", "-s", "workspace-write", "-c", "sandbox_workspace_write.network_access=true", "-o", "{svarsfil}", "{brief}"], brief: "arg", svar: "fil", stdin: "devnull", timeout_sekunder: 900, miljo: {}, mcp: false },
};

// ---------- konfiguration ----------
function rader(varden) {
  return (varden || []).join("\n");
}
function kommaseparerat(varden) {
  return (varden || []).join(", ");
}
function miljoRader(miljo) {
  return Object.entries(miljo || {}).map(([namn, varde]) => `${namn}=${varde}`).join("\n");
}
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
  data.agenter ||= {};
  data.regler ||= [];
  data.testserver ||= {};
  data.system ||= {};
  data.system.klarsprak_aktiv ??= false;
  data.system.klarsprak_sokvag ||= "";
  data.system.klarsprak_max_per100 ??= 3;
  data.krok ||= {};
  data.krok.anspraka ||= [];
  data.krok.slapp ||= [];
  data.krok.miljo ||= {};
  return data;
}


function renderaTestservrar() {
  const projektalias = new Set(konfigProjekt.map((projekt) => projekt.alias));
  const overgivna = Object.keys(konfig.testserver || {})
    .filter((alias) => !projektalias.has(alias))
    .sort((a, b) => a.localeCompare(b, "sv"))
    .map((alias) => ({ alias, name: alias, overgiven: true }));
  const kort = [...konfigProjekt, ...overgivna];
  $("#testserverkort").innerHTML = kort.map((projekt) => {
    const server = konfig.testserver[projekt.alias];
    const aktiv = Boolean(server);
    const t = server || {};
    const huvud = projekt.overgiven
      ? `<div class="testservernamn"><strong class="mono">${esc(projekt.alias)}</strong><span class="overgivenmarke">Övergivet</span><small>Projektet finns inte längre.</small></div>
        <button type="button" class="btn sm fara" data-ta-bort-testserver>Ta bort block</button>`
      : `<strong>${esc(projekt.name)} <span class="mono">(${esc(projekt.alias)})</span></strong>
        <span class="testserverhuvudval">
          <button type="button" class="btn sm" data-foresla-testserver="${esc(projekt.alias)}">Fråga agenten</button>
          <label class="kryss testserveraktiv"><input type="checkbox" data-testserverfalt="aktiv"${aktiv ? " checked" : ""}> Konfigurera testserver</label>
        </span>`;
    return `<article class="konfigkort testserverkort${projekt.overgiven ? " overgiven" : ""}" data-testserver="${esc(projekt.alias)}"${projekt.overgiven ? ' data-overgiven="true"' : ""}>
      <div class="korthuvud testserverhuvud">${huvud}</div>
      <fieldset class="testserverfalt"${aktiv ? "" : " disabled"}>
        <div class="faltgrid testservergrid">
          <label class="falt">Kommando<input data-testserverfalt="kommando" value="${esc(t.kommando || "")}" spellcheck="false"></label>
          <label class="falt">Arbetskatalog, valfri<input data-testserverfalt="cwd" value="${esc(t.cwd || "")}" placeholder="${esc(projekt.repo_path || "Projektets repo_path")}" spellcheck="false"></label>
          <label class="falt">Fast port, valfri<input data-testserverfalt="port" type="number" min="1" max="65535" value="${t.port || ""}"></label>
          <label class="falt">Hälsosökväg, valfri<input data-testserverfalt="halsa" value="${esc(t.halsa || "")}" placeholder="/" spellcheck="false"></label>
        </div>
        <label class="falt">Args, ett argument per rad<textarea data-testserverfalt="args" rows="4" spellcheck="false">${esc(rader(t.args))}</textarea></label>
        <label class="falt"><span>Miljö, en <code>NYCKEL=värde</code> per rad. Ett sparat värde visas som <code>***sparad***</code> och byts först när du skriver ett nytt</span><textarea data-testserverfalt="miljo" rows="3" spellcheck="false">${esc(miljoRader(t.miljo))}</textarea></label>
      </fieldset>
    </article>`;
  }).join("") || '<div class="tom">Inga projekt finns att konfigurera.</div>';
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
          <label class="falt">Modell, valfri<input data-agentfalt="modell" value="${esc(a.modell || "")}" spellcheck="false"></label>
          <label class="falt">Ansträngning, valfri<input data-agentfalt="anstrangning" value="${esc(a.anstrangning || "")}" spellcheck="false"></label>
          <label class="falt">Strömmande utdata<select data-agentfalt="strom"><option value=""${!a.strom ? " selected" : ""}>ingen</option><option value="claude-json"${a.strom === "claude-json" ? " selected" : ""}>claude-json</option><option value="codex-json"${a.strom === "codex-json" ? " selected" : ""}>codex-json</option></select></label>
          <label class="kryss detaljkryss"><input data-agentfalt="mcp" type="checkbox"${a.mcp ? " checked" : ""}> Lägg till MCP-konfiguration</label>
        </div>
        <label class="falt"><span>Miljö, en <code>NYCKEL=värde</code> per rad. Ett sparat värde visas som <code>***sparad***</code> och byts först när du skriver ett nytt</span><textarea data-agentfalt="miljo" rows="3" spellcheck="false">${esc(miljoRader(a.miljo))}</textarea></label>
      </details>
      <div class="provsvar" hidden></div>
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
    <div class="faltgrid regelgrid">
      <label class="falt">Modell, valfri<input data-regelfalt="modell" value="${esc(regel.modell || "")}" spellcheck="false"></label>
      <label class="falt">Ansträngning, valfri<input data-regelfalt="anstrangning" value="${esc(regel.anstrangning || "")}" spellcheck="false"></label>
    </div>
  </article>`).join("") || '<div class="tom">Inga regler. Den förvalda agenten används.</div>';

  renderaTestservrar();

  $("#klarsprakAktiv").checked = Boolean(konfig.system?.klarsprak_aktiv);
  $("#klarsprakSokvag").value = konfig.system?.klarsprak_sokvag || "";
  $("#klarsprakMax").value = String(konfig.system?.klarsprak_max_per100 ?? 3);
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
      strom: hamta("strom").value,
      modell: hamta("modell").value.trim(),
      anstrangning: hamta("anstrangning").value.trim(),
    };
  });
  const reglerNy = Array.from(document.querySelectorAll(".regelkort")).map((kort) => {
    const hamta = (falt) => kort.querySelector(`[data-regelfalt="${falt}"]`).value;
    return {
      namn: hamta("namn").trim(), agent: namnbyten[hamta("agent")] || hamta("agent"),
      typ: lasLista(hamta("typ"), ","), etiketter: lasLista(hamta("etiketter"), ","),
      nyckelord: lasLista(hamta("nyckelord"), ","),
      modell: hamta("modell").trim(), anstrangning: hamta("anstrangning").trim(),
    };
  });
  const testserverNy = {};
  document.querySelectorAll(".testserverkort").forEach((kort) => {
    const hamta = (falt) => kort.querySelector(`[data-testserverfalt="${falt}"]`);
    if (hamta("aktiv") && !hamta("aktiv").checked) return;
    const port = hamta("port").value.trim();
    testserverNy[kort.dataset.testserver] = {
      kommando: hamta("kommando").value.trim(),
      args: lasLista(hamta("args").value, "\n"),
      cwd: hamta("cwd").value.trim(),
      port: port ? Number(port) : 0,
      halsa: hamta("halsa").value.trim(),
      miljo: lasMiljo(hamta("miljo").value, `Testservermiljön för ${kort.dataset.testserver}`),
    };
  });
  const forval = namnbyten[$("#defaultAgent").value] || $("#defaultAgent").value;
  return {
    // Metadatan följer med utkastet, annars tappar metaraden sökvägen.
    sokvag: konfig?.sokvag,
    saknas: konfig?.saknas,
    default_agent: forval, agenter: agenterNy, regler: reglerNy,
    testserver: testserverNy,
    raderade_testservrar: Array.from(raderadeTestservrar),
    system: {
      klarsprak_aktiv: $("#klarsprakAktiv").checked,
      klarsprak_sokvag: $("#klarsprakSokvag").value.trim(),
      klarsprak_max_per100: Number($("#klarsprakMax").value),
    },
    krok: {
      anspraka: lasLista($("#krokAnspraka").value, "\n"),
      slapp: lasLista($("#krokSlapp").value, "\n"),
      miljo: lasMiljo($("#krokMiljo").value, "Krokens miljö"),
    },
  };
}

async function laddaKonfig() {
  try {
    const [konfigdata, projektdata] = await Promise.all([hamta("/api/konfig"), hamta("/api/projects")]);
    konfig = normaliseraKonfig(konfigdata);
    raderadeTestservrar.clear();
    konfigProjekt = (projektdata.projects || []).slice().sort((a, b) => a.name.localeCompare(b.name, "sv"));
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
$("#testserverkort").addEventListener("change", (e) => {
  const aktiv = e.target.closest('[data-testserverfalt="aktiv"]');
  if (!aktiv) return;
  aktiv.closest(".testserverkort").querySelector(".testserverfalt").disabled = !aktiv.checked;
});
// foreslaTestserver ber en agent läsa projektet och fylla i fälten. Inget
// sparas, användaren granskar förslaget och trycker på Spara pm.toml.
async function foreslaTestserver(knapp) {
  const kort = knapp.closest(".testserverkort");
  const falt = (namn) => kort.querySelector(`[data-testserverfalt="${namn}"]`);
  const gammalText = knapp.textContent;
  knapp.disabled = true;
  knapp.textContent = "Agenten läser projektet...";
  try {
    const f = await hamta(`/api/projekt/${encodeURIComponent(knapp.dataset.foreslaTestserver)}/foresla-testserver`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });
    falt("aktiv").checked = true;
    kort.querySelector(".testserverfalt").disabled = false;
    falt("kommando").value = f.kommando;
    falt("args").value = (f.args || []).join("\n");
    falt("cwd").value = f.cwd || "";
    falt("halsa").value = f.halsa || "";
    falt("port").value = f.port ? String(f.port) : "";
    toast(f.forklaring ? `Förslag: ${f.forklaring} Granska och spara.` : "Förslaget är ifyllt. Granska och spara.");
  } catch (err) {
    toast(err.message);
  } finally {
    knapp.disabled = false;
    knapp.textContent = gammalText;
  }
}

$("#v-konfig").addEventListener("click", async (e) => {
  const forslagsknapp = e.target.closest("[data-foresla-testserver]");
  if (forslagsknapp) return foreslaTestserver(forslagsknapp);
  const agentkort = e.target.closest(".agentkort");
  const regelkort = e.target.closest(".regelkort");
  const testserverkort = e.target.closest(".testserverkort");
  if (e.target.closest("[data-ta-bort-testserver]")) {
    try { konfig = samlaKonfig(); } catch (err) { return toast(err.message); }
    const alias = testserverkort.dataset.testserver;
    delete konfig.testserver[alias];
    raderadeTestservrar.add(alias);
    renderaKonfig();
    return;
  }
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
      const statusnamn = { ok: "Godkänd", fel: "Fel", overhoppad: "Överhoppad" };
      ruta.innerHTML = `<ul class="provresultat">${(resultat.delresultat || []).map((del) =>
        `<li class="prov-${esc(del.status)}"><strong>${esc(del.namn)}: ${esc(statusnamn[del.status] || del.status)}</strong><span>${esc(del.meddelande)}</span></li>`
      ).join("")}</ul>${resultat.svar ? `<details><summary>Visa agentsvaret</summary><pre>${esc(resultat.svar)}</pre></details>` : ""}`;
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
  if (!konfig) {
    $("#konfigFel").textContent = "Konfigurationen är inte inläst än. Ladda om sidan innan du sparar.";
    return;
  }
  knapp.disabled = true;
  try {
    const utkast = samlaKonfig();
    konfig = utkast;
    konfig = normaliseraKonfig(await hamta("/api/konfig", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(utkast) }));
    agenter = Object.keys(konfig.agenter || {}).sort();
    agentutkast.clear();
    raderadeTestservrar.clear();
    renderaKonfig();
    toast("pm.toml är sparad.");
  } catch (err) {
    $("#konfigFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
});


// Förslagsmodulen laddas efter den här filen. Koppla in poängen när hela sidan finns.
window.addEventListener("DOMContentLoaded", () => {
  const visaUtkast = window.visaTaskutkast;
  if (typeof visaUtkast !== "function") return;
  window.visaTaskutkast = (utkast) => {
    visaUtkast(utkast);
    const rad = $("#utkastKlarsprak");
    if (typeof utkast.klarsprak_poang !== "number") {
      rad.hidden = true;
      rad.textContent = "";
      return;
    }
    const omskriven = utkast.klarsprak_omskriven ? " efter en omskrivning" : "";
    rad.textContent = `Klarspråk: ${utkast.klarsprak_poang.toFixed(2)} fynd per hundra ord${omskriven}.`;
    rad.hidden = false;
  };
});
