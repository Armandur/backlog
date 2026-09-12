// Redigera, etikettera och ta bort en task. Laddas efter pm.js.
let redigerarRef = "";

function stangRedigera() {
  $("#redigeradrawer").classList.remove("on");
  if (!$("#drawer").classList.contains("on") && !$("#kommentarsdrawer").classList.contains("on")
      && !$("#forslagsdrawer").classList.contains("on")) {
    $("#scrim").classList.remove("on");
  }
}

function visaEtiketter(data) {
  const rad = $("#redEtiketter");
  rad.replaceChildren();
  for (const etikett of data.etiketter || []) {
    const chip = document.createElement("span");
    chip.className = "etikett";
    chip.textContent = etikett.name;
    const bort = document.createElement("button");
    bort.type = "button";
    bort.className = "x";
    bort.title = `Ta bort ${etikett.name}`;
    bort.textContent = "×";
    bort.dataset.taBortEtikett = etikett.name;
    chip.append(bort);
    rad.append(chip);
  }
  if (!(data.etiketter || []).length) {
    const tom = document.createElement("span");
    tom.className = "meta";
    tom.textContent = "Inga etiketter än.";
    rad.append(tom);
  }
  const lista = $("#redEtikettforslag");
  lista.replaceChildren();
  for (const etikett of data.valbara_etiketter || []) {
    const val = document.createElement("option");
    val.value = etikett.name;
    lista.append(val);
  }
}

function visaPlaner(planer) {
  const ruta = $("#redPlan");
  // Texten ligger i planens aktuella version, inte på planen själv.
  const version = ((planer || [])[0] || {}).version;
  if (!version) {
    ruta.replaceChildren();
    ruta.textContent = "Ingen plan är fäst på tasken.";
    return;
  }
  PMMarkdown.rendera(ruta, `## ${version.title}\n\n${version.body || ""}`);
}

async function oppnaRedigera(ref) {
  stangDela();
  stangKommentarer();
  stangTaskforslag();
  redigerarRef = ref;
  $("#redigeraTitel").textContent = `${ref} · redigera`;
  $("#redFel").textContent = "";
  $("#redigeradrawer").classList.add("on");
  $("#scrim").classList.add("on");
  try {
    const t = await hamta(`/api/tasks/${encodeURIComponent(ref)}/detaljer`);
    if (redigerarRef !== ref) return;
    $("#redTitel").value = t.titel;
    $("#redBeskrivning").value = t.beskrivning || "";
    $("#redTyp").value = t.typ;
    $("#redPrioritet").value = String(t.prioritet);
    $("#redStatus").value = t.status;
    $("#redPlanTitel").value = "";
    $("#redPlanText").value = "";
    visaEtiketter(t);
    visaPlaner(t.planer);
  } catch (err) {
    $("#redFel").textContent = err.message;
  }
}

$("#redigeraStang").addEventListener("click", stangRedigera);
$("#scrim").addEventListener("click", stangRedigera);

document.body.addEventListener("click", (event) => {
  const knapp = event.target.closest("[data-redigera]");
  if (knapp) oppnaRedigera(knapp.dataset.redigera);
});

$("#redigeraform").addEventListener("submit", async (event) => {
  event.preventDefault();
  const ref = redigerarRef;
  $("#redSpara").disabled = true;
  $("#redFel").textContent = "";
  try {
    await hamta(`/api/tasks/${encodeURIComponent(ref)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        titel: $("#redTitel").value,
        beskrivning: $("#redBeskrivning").value,
        typ: $("#redTyp").value,
        prioritet: Number($("#redPrioritet").value),
        status: $("#redStatus").value,
      }),
    });
    await laddaOversikt();
    stangRedigera();
    toast(`${ref} är sparad.`);
  } catch (err) {
    $("#redFel").textContent = err.message;
  } finally {
    $("#redSpara").disabled = false;
  }
});

$("#redLaggEtikett").addEventListener("click", async () => {
  const namn = $("#redEtikett").value.trim();
  if (!namn) return;
  try {
    const data = await hamta(`/api/tasks/${encodeURIComponent(redigerarRef)}/etiketter`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ namn }),
    });
    $("#redEtikett").value = "";
    visaEtiketter(data);
    await laddaOversikt();
  } catch (err) {
    $("#redFel").textContent = err.message;
  }
});

$("#redEtiketter").addEventListener("click", async (event) => {
  const knapp = event.target.closest("[data-ta-bort-etikett]");
  if (!knapp) return;
  try {
    const data = await hamta(
      `/api/tasks/${encodeURIComponent(redigerarRef)}/etiketter/${encodeURIComponent(knapp.dataset.taBortEtikett)}`,
      { method: "DELETE" });
    visaEtiketter(data);
    await laddaOversikt();
  } catch (err) {
    $("#redFel").textContent = err.message;
  }
});

$("#redSparaPlan").addEventListener("click", async () => {
  const innehall = $("#redPlanText").value.trim();
  if (!innehall) {
    $("#redFel").textContent = "Skriv planens innehåll.";
    return;
  }
  try {
    await hamta(`/api/tasks/${encodeURIComponent(redigerarRef)}/plan`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ titel: $("#redPlanTitel").value, innehall }),
    });
    const t = await hamta(`/api/tasks/${encodeURIComponent(redigerarRef)}/detaljer`);
    visaPlaner(t.planer);
    $("#redPlanText").value = "";
    toast("Planen är sparad.");
  } catch (err) {
    $("#redFel").textContent = err.message;
  }
});

$("#redTaBort").addEventListener("click", async () => {
  const ref = redigerarRef;
  if (!confirm(`Ta bort ${ref}? Tasken och dess planer och kommentarer försvinner.`)) return;
  try {
    await hamta(`/api/tasks/${encodeURIComponent(ref)}`, { method: "DELETE" });
    await laddaOversikt();
    stangRedigera();
    toast(`${ref} är borttagen.`);
  } catch (err) {
    $("#redFel").textContent = err.message;
  }
});
