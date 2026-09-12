const aktivaForslagsstrommar = new WeakMap();

function avbrytForslagsstrom(status) {
  const lage = aktivaForslagsstrommar.get(status);
  if (!lage) return;
  lage.kalla.close();
  clearInterval(lage.timer);
  aktivaForslagsstrommar.delete(status);
}

function skapaForslagskort(status) {
  avbrytForslagsstrom(status);
  const gammalt = status.parentElement.querySelector(`[data-forslagskort="${status.id}"]`);
  if (gammalt) gammalt.remove();
  status.classList.add("forslagsstatus");
  const snurra = document.createElement("span");
  snurra.className = "snurra";
  snurra.setAttribute("aria-hidden", "true");
  status.replaceChildren(snurra, document.createTextNode(" Agenten arbetar."));
  status.hidden = false;

  const kort = document.createElement("div");
  kort.className = "arbetskort arbetar";
  kort.dataset.forslagskort = status.id;
  const rad = document.createElement("div");
  rad.className = "arbetsrad";
  const arbetsstatus = document.createElement("span");
  arbetsstatus.className = "arbetsstatus";
  arbetsstatus.textContent = "Agenten startar";
  const meta = document.createElement("span");
  meta.className = "arbetsmeta";
  rad.append(arbetsstatus, meta);
  const lista = document.createElement("ol");
  lista.className = "samtalshandelser";
  lista.setAttribute("aria-live", "polite");
  kort.append(rad, lista);
  status.insertAdjacentElement("afterend", kort);
  return { kort, lista, arbetsstatus, meta };
}

function startaForslagsjobb(status, url, kropp) {
  const vy = skapaForslagskort(status);
  const start = Date.now();
  let tokens = 0;
  return hamta(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(kropp),
  }).then((svar) => new Promise((resolve, reject) => {
    const korningId = svar.korning.id;
    const kalla = new EventSource(`/api/korningar/${encodeURIComponent(korningId)}/strom`);
    const uppdateraMeta = () => {
      const delar = [tidtext(start)];
      if (tokens) delar.push(tokentext(tokens));
      vy.meta.textContent = delar.join(" · ");
    };
    const laggTill = (event) => {
      const handelse = JSON.parse(event.data);
      if (handelse.sort === "tokens" || handelse.sort === "tokens_total") {
        const antal = Number(handelse.text) || 0;
        tokens = handelse.sort === "tokens_total" ? antal : tokens + antal;
      } else {
        vy.lista.append(samtalshandelseElement(handelse));
        vy.arbetsstatus.textContent = handelse.text.split("\n")[0];
        skrollaNed(vy.lista);
      }
      uppdateraMeta();
    };
    const avsluta = () => {
      kalla.close();
      clearInterval(lage.timer);
      aktivaForslagsstrommar.delete(status);
      vy.kort.classList.remove("arbetar");
      vy.kort.classList.add("avslutad");
      status.replaceChildren(document.createTextNode("Förslaget är klart."));
    };
    const lage = { kalla, timer: setInterval(uppdateraMeta, 1000) };
    aktivaForslagsstrommar.set(status, lage);
    uppdateraMeta();
    kalla.onmessage = (event) => {
      try { laggTill(event); } catch { toast("PM kunde inte läsa agentens händelse."); }
    };
    kalla.addEventListener("slut", async (event) => {
      try {
        laggTill(event);
        avsluta();
        resolve(await hamta(`/api/forslag/${encodeURIComponent(korningId)}`));
      } catch (err) {
        avsluta();
        reject(err);
      }
    });
    kalla.onerror = () => {
      avsluta();
      reject(new Error("Anslutningen till agenten bröts."));
    };
  })).catch((err) => {
    status.replaceChildren();
    vy.kort.classList.remove("arbetar");
    vy.kort.classList.add("avslutad");
    if (!vy.lista.children.length) {
      vy.lista.append(samtalshandelseElement({ tid: Date.now() * 1e6, sort: "fel", text: err.message }));
    }
    throw err;
  });
}

let taskforslagRef = "";
let autoTaskPagar = false;
const TASKLAGE_NYCKEL = "backlog-pm-tasklage";
const TASKTYP_ETIKETT = {
  task: "task", bug: "bugg", issue: "problem", improvement: "förbättring",
  feature: "funktion", vulnerability: "sårbarhet", chore: "underhåll",
  spike: "utredning", "bucket-list": "idé",
};

function sparatTasklage() {
  try {
    return localStorage.getItem(TASKLAGE_NYCKEL);
  } catch {
    return null;
  }
}

function visaTasklage(lage) {
  const auto = lage !== "avancerat";
  $("#autotaskform").hidden = !auto;
  $("#taskform").hidden = auto;
  document.querySelector(`input[name="tasklage"][value="${auto ? "auto" : "avancerat"}"]`).checked = true;
}

function sparaTasklage(lage) {
  try {
    localStorage.setItem(TASKLAGE_NYCKEL, lage);
  } catch {
    // Läget fungerar fortfarande under besöket när webbläsaren nekar lagring.
  }
}

function sattAutoStatus(text, fel = false) {
  const status = $("#autoTaskStatus");
  status.textContent = text;
  status.classList.toggle("fel-text", fel);
}

async function laddaAutoOversikt() {
  try {
    await laddaOversikt();
  } catch (err) {
    console.error("Kunde inte uppdatera tasklistan:", err);
  }
}

document.querySelectorAll('input[name="tasklage"]').forEach((val) => {
  val.addEventListener("change", (event) => {
    const lage = event.target.value;
    visaTasklage(lage);
    sparaTasklage(lage);
  });
});
visaTasklage(sparatTasklage() || "auto");

$("#autotaskform").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (autoTaskPagar) return;

  const formular = $("#autotaskform");
  const knapp = $("#skapaAutoTask");
  const text = $("#autoTaskText").value;
  autoTaskPagar = true;
  knapp.disabled = true;
  sattAutoStatus("PM skapar ett förslag...");

  try {
    const forslag = await startaForslagsjobb($("#autoTaskStatus"),
      `/api/projects/${encodeURIComponent(alias)}/foresla-task`, { text });
    sattAutoStatus("Förslaget är klart. PM lägger till tasken...");
    const task = await hamta(`/api/projects/${encodeURIComponent(alias)}/tasks`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        titel: forslag.titel,
        beskrivning: forslag.beskrivning,
        typ: forslag.typ,
        prioritet: forslag.prioritet,
      }),
    });
    formular.reset();
    const typ = TASKTYP_ETIKETT[forslag.typ] || forslag.typ;
    sattAutoStatus(`${task.ref} skapad som ${typ} P${forslag.prioritet} med agentens titel och beskrivning.`);
    await laddaAutoOversikt();
  } catch (err) {
    sattAutoStatus(err.message, true);
  } finally {
    autoTaskPagar = false;
    knapp.disabled = false;
  }
});

function stangTaskforslag() {
  avbrytForslagsstrom($("#taskforslagsStatus"));
  $("#forslagsdrawer").classList.remove("on");
  if (!$("#drawer").classList.contains("on") && !$("#kommentarsdrawer").classList.contains("on")) {
    $("#scrim").classList.remove("on");
  }
}

function visaTaskutkast(utkast) {
  $("#utkastTitel").value = utkast.titel;
  $("#utkastBeskrivning").value = utkast.beskrivning;
  $("#utkastTyp").value = utkast.typ;
  $("#utkastPrioritet").value = String(utkast.prioritet);
  $("#utkastModell").value = utkast.modell || "";
  $("#utkastAnstrangning").value = utkast.anstrangning || "";
  $("#taskutkast").hidden = false;
  $("#sparaTaskforslag").disabled = false;
  $("#forslagDela").disabled = false;
}

async function hamtaTaskforslag(sort) {
  const ref = taskforslagRef;
  const agent = $("#forslagAgent").value;
  $("#taskforslagsFel").textContent = "";
  $("#taskforslagsStatus").hidden = false;
  $("#sparaTaskforslag").disabled = true;
  $("#forslagDela").disabled = true;
  try {
    const utkast = await startaForslagsjobb($("#taskforslagsStatus"),
      `/api/tasks/${encodeURIComponent(ref)}/foresla`, { sort, agent });
    if (taskforslagRef !== ref) return;
    visaTaskutkast(utkast);
  } catch (err) {
    if (taskforslagRef === ref) $("#taskforslagsFel").textContent = err.message;
  } finally {
    if (taskforslagRef === ref) $("#taskforslagsStatus").hidden = true;
  }
}

function oppnaTaskforslag(ref, sort) {
  stangDela();
  stangKommentarer();
  taskforslagRef = ref;
  $("#forslagTitel").textContent = `${ref} · förslag`;
  $("#forslagAgent").innerHTML = `<option value="">förvald agent</option>` +
    agenter.map((agent) => `<option value="${esc(agent)}">${esc(agent)}</option>`).join("");
  $("#taskutkast").hidden = true;
  $("#taskforslagsFel").textContent = "";
  $("#forslagsdrawer").classList.add("on");
  $("#scrim").classList.add("on");
  hamtaTaskforslag(sort);
}

$("#forslagStang").addEventListener("click", stangTaskforslag);
$("#scrim").addEventListener("click", stangTaskforslag);

document.body.addEventListener("click", (event) => {
  const knapp = event.target.closest("[data-taskforslag]");
  if (knapp) oppnaTaskforslag(knapp.dataset.taskref, knapp.dataset.taskforslag);
  const hamtaKnapp = event.target.closest("[data-hamta-forslag]");
  if (hamtaKnapp) hamtaTaskforslag(hamtaKnapp.dataset.hamtaForslag);
});

$("#taskforslagsform").addEventListener("submit", async (event) => {
  event.preventDefault();
  const knapp = $("#sparaTaskforslag");
  const ref = taskforslagRef;
  knapp.disabled = true;
  $("#taskforslagsFel").textContent = "";
  try {
    await hamta(`/api/tasks/${encodeURIComponent(ref)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        titel: $("#utkastTitel").value,
        beskrivning: $("#utkastBeskrivning").value,
        typ: $("#utkastTyp").value,
        prioritet: Number($("#utkastPrioritet").value),
      }),
    });
    await laddaOversikt();
    stangTaskforslag();
    toast(`${ref} har sparats med dina granskade värden.`);
  } catch (err) {
    $("#taskforslagsFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
});

$("#forslagDela").addEventListener("click", () => {
  const ref = taskforslagRef;
  const modell = $("#utkastModell").value;
  const anstrangning = $("#utkastAnstrangning").value;
  stangTaskforslag();
  oppnaDela(ref);
  $("#dModell").value = modell;
  $("#dAnstrangning").value = anstrangning;
});


$("#forslagsform").addEventListener("submit", async (event) => {
  event.preventDefault();
  const knapp = $("#hamtaForslag");
  $("#forslagsFel").textContent = "";
  knapp.disabled = true;
  try {
    konfig = samlaKonfig();
    const forslag = await startaForslagsjobb($("#forslagsStatus"), "/api/konfig/foresla", {
      alias, beskrivning: $("#verktygsbeskrivning").value,
    });
    const { namn: basnamn, ...agent } = forslag;
    let namn = basnamn;
    let nummer = 2;
    while (konfig.agenter[namn]) namn = basnamn + "-" + nummer++;
    konfig.agenter[namn] = agent;
    agentutkast.add(namn);
    if (!konfig.default_agent) konfig.default_agent = namn;
    renderaKonfig();
  } catch (err) {
    $("#forslagsFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
    $("#forslagsStatus").hidden = true;
  }
});
