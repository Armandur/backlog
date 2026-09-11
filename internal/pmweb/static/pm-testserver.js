// Testserverns start, stopp och status i projektvyn. Laddas efter pm.js.
let testserverLever = false;
async function laddaTestserver() {
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/testserver`);
  testserverLever = data.lever;
  const knapp = $("#testserverKnapp");
  knapp.textContent = data.lever ? "Stoppa" : "Starta";
  knapp.disabled = !data.konfigurerad;
  $("#testserverStatus").textContent = !data.konfigurerad ? "Projektet saknar testserverkonfiguration." : data.lever ? `Kör på port ${data.port}. Logg: ${data.logg_sokvag}` : "Testservern kör inte.";
  const lank = $("#testserverLank");
  lank.hidden = !data.lever || !data.lank;
  lank.href = data.lank || "";
}
$("#testserverKnapp").onclick = async () => {
  const skaStoppa = testserverLever;
  const knapp = $("#testserverKnapp");
  knapp.disabled = true;
  try {
    await hamta(`/api/projects/${encodeURIComponent(alias)}/testserver/${skaStoppa ? "stop" : "start"}`, { method: "POST" });
    toast(skaStoppa ? "Testservern har stoppats." : "Testservern har startats.");
    await laddaTestserver();
  } catch (err) {
    toast(err.message);
  } finally {
    knapp.disabled = false;
  }
};
