// Testserverns start, stopp och status i projektvyn. Laddas efter pm.js.
const TESTSERVER_STATUS = {
  nere: { etikett: "Nere", klass: "p-koad" },
  startar: { etikett: "Startar", klass: "p-kor" },
  uppe: { etikett: "Uppe", klass: "p-klar" },
  krasch: { etikett: "Krasch", klass: "p-fel" },
};
let testserverLever = false;

function sattTestserverBadge(status) {
  const lage = TESTSERVER_STATUS[status] || TESTSERVER_STATUS.nere;
  const badge = $("#testserverBadge");
  badge.className = `pill ${lage.klass}`;
  badge.textContent = lage.etikett;
}

async function laddaTestserver() {
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/testserver`);
  testserverLever = data.lever;
  sattTestserverBadge(data.status);
  const knapp = $("#testserverKnapp");
  knapp.textContent = data.lever ? "Stoppa" : "Starta";
  knapp.disabled = !data.konfigurerad;
  if (!data.konfigurerad) {
    $("#testserverStatus").textContent = "Projektet saknar testserverkonfiguration.";
  } else if (data.status === "uppe") {
    $("#testserverStatus").textContent = `Svarar på port ${data.port}. Logg: ${data.logg_sokvag}`;
  } else if (data.status === "startar") {
    $("#testserverStatus").textContent = `Processen kör på port ${data.port}, men servern svarar inte ännu.`;
  } else if (data.status === "krasch") {
    const exitkod = data.exitkod === undefined ? "okänd" : data.exitkod;
    $("#testserverStatus").textContent = `Processen har avslutats. Exitkod: ${exitkod}. Logg: ${data.logg_sokvag}`;
  } else {
    $("#testserverStatus").textContent = "Testservern kör inte.";
  }
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
