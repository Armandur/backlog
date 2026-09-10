// Konfigvyn: agenter, regler och krok. Laddas efter pm.js.
let konfig = null;
const agentutkast = new Set();
const AGENTMALLAR = {
  tom: { namn: "ny-agent", kommando: "", args: ["{brief}"], brief: "arg", svar: "stdout", stdin: "devnull", timeout_sekunder: 900, miljo: {}, mcp: false },
  claude: { namn: "claude", kommando: "claude", args: ["-p", "{brief}"], brief: "arg", svar: "stdout", stdin: "devnull", timeout_sekunder: 900, miljo: {}, mcp: true },
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
  data.krok ||= {};
  data.krok.anspraka ||= [];
  data.krok.slapp ||= [];
  data.krok.miljo ||= {};
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
    renderaKonfig();
    toast("pm.toml är sparad.");
  } catch (err) {
    $("#konfigFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
});
