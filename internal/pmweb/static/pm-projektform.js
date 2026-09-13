// Formuläret för nytt projekt, med agentens förslag på startkommando.
// Laddas efter pm.js.
// Knappen gäller bara ett repo som redan finns. Ett nytt projekt har ingen kod
// att läsa förrän PM skapat mappen.
function visaStartforslag() {
  const lage = document.querySelector('input[name="lage"]:checked').value;
  const githubFalt = $("#projektGitHubFalt");
  const githubRepo = $("#projektGitHubRepo");
  if (githubFalt) githubFalt.hidden = lage !== "github";
  if (githubRepo) githubRepo.required = lage === "github";
  const tips = {
    nytt: "PM skapar katalogen, ett Git-repo och README.md.",
    befintligt: "PM kopplar katalogens befintliga Git-repo.",
    github: "PM klonar GitHub-repot till den här katalogen.",
  };
  $("#projektSokvagTips").textContent = tips[lage] || tips.nytt;
  $("#foreslaStart").hidden = lage !== "befintligt";
}
document.querySelectorAll('input[name="lage"]').forEach((val) => val.addEventListener("change", visaStartforslag));
visaStartforslag();

$("#foreslaStart").addEventListener("click", async () => {
  const knapp = $("#foreslaStart");
  const sokvag = $("#projektSokvag").value.trim();
  if (!sokvag) {
    $("#projektFel").textContent = "Fyll i sökvägen först, annars vet agenten inte var koden ligger.";
    return;
  }
  const gammalText = knapp.textContent;
  knapp.disabled = true;
  knapp.textContent = "Agenten läser projektet...";
  $("#projektFel").textContent = "";
  try {
    const f = await hamta("/api/foresla-testserver", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sokvag }),
    });
    $("#projektStart").value = [f.kommando, ...(f.args || [])].join(" ");
    if (f.forklaring) toast(f.forklaring);
  } catch (err) {
    $("#projektFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
    knapp.textContent = gammalText;
  }
});

$("#projektform").addEventListener("submit", async (e) => {
  e.preventDefault();
  const knapp = $("#skapaProjekt");
  $("#projektFel").textContent = "";
  $("#projektSvar").hidden = true;
  knapp.disabled = true;
  try {
    const data = await hamta("/api/projekt", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        alias: $("#projektAlias").value,
        namn: $("#projektNamn").value,
        beskrivning: $("#projektBeskrivning").value,
        lage: document.querySelector('input[name="lage"]:checked').value,
        sokvag: $("#projektSokvag").value,
        repo: $("#projektGitHubRepo")?.value || "",
        startkommando: $("#projektStart").value,
      }),
    });
    $("#projektLank").href = data.lank;
    $("#projektLank").textContent = `Öppna ${data.projekt.name}`;
    $("#projektSvar").hidden = false;
    if (data.varning) toast(data.varning);
  } catch (err) {
    $("#projektFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
});
