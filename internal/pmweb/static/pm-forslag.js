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
    const forslag = await hamta(`/api/projects/${encodeURIComponent(alias)}/foresla-task`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text }),
    });
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
    const utkast = await hamta(`/api/tasks/${encodeURIComponent(ref)}/foresla`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sort, agent }),
    });
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
