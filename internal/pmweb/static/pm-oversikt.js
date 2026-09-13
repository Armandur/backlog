// Den globala översikten byggs här för att hålla pm.html liten.
const oversiktsRuta = $("#v-oversikt");
oversiktsRuta.innerHTML = `
  <h1>Översikt</h1>
  <p class="lead">Följ arbetet och dela ut tasks i alla aktiva projekt.</p>
  <form id="oversiktsUtdelning" class="oversiktsutdelning">
    <h2>Dela ut en task</h2>
    <div class="oversiktsutdelningsfalt">
      <label class="falt" for="oversiktsProjekt">Projekt
        <select id="oversiktsProjekt" required></select>
      </label>
      <label class="falt" for="oversiktsTask">Task
        <input id="oversiktsTask" required spellcheck="false" placeholder="TASK-123">
      </label>
      <label class="falt" for="oversiktsAgent">Agent, valfri
        <select id="oversiktsAgent"></select>
      </label>
      <button class="btn pri" type="submit">Dela ut</button>
    </div>
    <p class="fel-text" id="oversiktsUtdelningsfel" role="alert"></p>
  </form>
  <div class="oversiktsgrid">
    <section><h2>Pågående <span class="n" id="allaPagaendeAntal"></span></h2><div class="list" id="allaPagaende"></div></section>
    <section><h2>Kö <span class="n" id="allaKoAntal"></span></h2><div class="list" id="allaKo"></div></section>
    <section class="oversiktsbred"><h2>Väntar på mig <span class="n" id="allaVantarAntal"></span></h2><div class="list" id="allaVantar"></div></section>
    <section><h2>Agenternas kvotläge</h2><div class="list" id="allaKvoter"></div></section>
    <section><h2>Testservrar</h2><div class="list" id="allaTestservrar"></div></section>
  </div>`;

let allaOversiktPollning = false;
const oversiktsTestserverstatus = {
  nere: ["Nere", "p-koad"], startar: ["Startar", "p-kor"],
  uppe: ["Uppe", "p-klar"], krasch: ["Krasch", "p-fel"],
};

function oversiktsProjektLank(projekt, hash) {
  const mal = `/pm/${encodeURIComponent(projekt.alias)}#${hash || "projekt"}`;
  return `<a class="projektref" href="${mal}">${esc(projekt.namn)}</a>`;
}

function oversiktsKorningsrad(k, ko) {
  const detalj = ko ? esc(k.koorsak) : `${esc(k.agent)} · startade ${esc(klocka(k.startad_at || k.skapad_at))}`;
  const arbete = k.task_ref
    ? taskLank(k.task_ref)
    : `<a class="mono ref" href="/pm/${encodeURIComponent(k.projekt.alias)}#samtal">Samtal</a>`;
  return rad(`${oversiktsProjektLank(k.projekt, "projekt")}
    <div class="t">${arbete} <span class="meta">${detalj}</span></div>
    <div class="act">${pill(k.status)}<a class="btn sm" href="/pm/${encodeURIComponent(k.projekt.alias)}#korningar">Öppna</a></div>`);
}

function ritaAllaKorningar(korningar) {
  const pagaende = korningar.filter((k) => k.status === "kor");
  const ko = korningar.filter((k) => k.status === "koad");
  $("#allaPagaendeAntal").textContent = pagaende.length;
  $("#allaKoAntal").textContent = ko.length;
  fyll("#allaPagaende", pagaende, (k) => oversiktsKorningsrad(k, false), "Inga körningar pågår.");
  fyll("#allaKo", ko, (k) => oversiktsKorningsrad(k, true), "Kön är tom.");
}

function ritaAllaVantar(poster) {
  $("#allaVantarAntal").textContent = poster.length;
  fyll("#allaVantar", poster, (v) => {
    const ref = v.ref ? taskLank(v.ref) : `<span class="pill p-fraga">Fråga</span>`;
    const hash = v.sort === "fraga" ? "samtal" : "projekt";
    const el = rad(`${oversiktsProjektLank(v.projekt, hash)}
      <div class="t">${ref}<span class="meta">${esc(v.text)}</span></div>
      <div class="act"><span class="pill p-${esc(v.sort)}">${esc(VANTAR_ETIKETT[v.sort] || v.sort)}</span><a class="btn sm" href="/pm/${encodeURIComponent(v.projekt.alias)}#${hash}">Öppna</a></div>`);
    el.classList.add("stripe", v.sort);
    return el;
  }, "Inget väntar på dig.");
}

// Nollställningen är det som avgör om en full kvot spelar roll. Har tiden
// passerat är siffran inaktuell, och då säger raden det i stället för att
// låtsas att kvoten fortfarande är tagen.
function nollstallning(ns) {
  const nar = Number(ns) / 1e6;
  if (!nar) return "";
  if (nar <= Date.now()) return ", nollställd";
  const dygn = 24 * 60 * 60 * 1000;
  if (nar - Date.now() < dygn) return `, nollställs ${klocka(ns)}`;
  return `, nollställs ${new Date(nar).toLocaleString("sv-SE", { weekday: "short", hour: "2-digit", minute: "2-digit" })}`;
}

function kvotText(fonster) {
  if (!fonster) return "saknas";
  return `${Math.round(fonster.andel * 100)} %${nollstallning(fonster.nollstalls_at)}`;
}

function ritaAllaKvoter(poster) {
  fyll("#allaKvoter", poster, (a) => {
    let text = `5h: ${kvotText(a.fem_timmar)} · 7d: ${kvotText(a.sju_dagar)}`;
    if (a.saknas) {
      text = a.rapporterar
        ? "Inget kvotläge har lästs av ännu."
        : "Agenten rapporterar inget kvotläge.";
    } else if (a.avlast_at) {
      text += ` · avläst ${klocka(a.avlast_at)}`;
    }
    const el = rad(`<span class="mono">${esc(a.agent)}</span><div class="t"><span class="meta">${esc(text)}</span></div>`);
    el.classList.add("kvotrad");
    return el;
  }, "Inga agenter är konfigurerade.");
}

function ritaAllaTestservrar(poster) {
  fyll("#allaTestservrar", poster, (s) => {
    let status = oversiktsTestserverstatus[s.status] || oversiktsTestserverstatus.nere;
    let text = "Testservern kör inte.";
    if (s.fel) {
      status = ["Okänd", "p-fel"];
      text = s.fel;
    } else if (!s.konfigurerad) {
      text = "Projektet saknar testserver.";
    } else if (s.status === "uppe") {
      text = `Svarar på port ${s.port}.`;
    } else if (s.status === "startar") {
      text = `Startar på port ${s.port}.`;
    } else if (s.status === "krasch") {
      text = "Processen har avslutats.";
    }
    const oppna = s.lank ? `<a class="btn sm" href="${esc(s.lank)}" target="_blank" rel="noopener">Öppna servern</a>` : "";
    return rad(`${oversiktsProjektLank(s.projekt, "projekt")}
      <div class="t"><span class="meta">${esc(text)}</span></div>
      <div class="act"><span class="pill ${status[1]}">${status[0]}</span>${oppna}</div>`);
  }, "Det finns inga aktiva projekt.");
}

function ritaOversiktsUtdelning(projekt) {
  const projektval = $("#oversiktsProjekt");
  const valt = projektval.value;
  projektval.innerHTML = projekt.length
    ? projekt.map((p) => `<option value="${esc(p.alias)}">${esc(p.namn)} (${esc(p.alias)})</option>`).join("")
    : `<option value="">Inga aktiva projekt</option>`;
  if (projekt.some((p) => p.alias === valt)) projektval.value = valt;
  $("#oversiktsAgent").innerHTML = `<option value="">regelvald agent</option>` +
    agenter.map((a) => `<option value="${esc(a)}">${esc(a)}</option>`).join("");
}

async function laddaAllaOversikt() {
  if (allaOversiktPollning) return;
  allaOversiktPollning = true;
  try {
    const [data, anvandning] = await Promise.all([hamta("/api/oversikt"), hamta("/api/anvandning")]);
    ritaOversiktsUtdelning(data.projekt || []);
    ritaAllaKorningar(data.korningar || []);
    ritaAllaVantar(data.vantar || []);
    ritaAllaKvoter(anvandning.anvandning || []);
    ritaAllaTestservrar(data.testservrar || []);
  } finally {
    allaOversiktPollning = false;
  }
}

$("#oversiktsUtdelning").addEventListener("submit", async (event) => {
  event.preventDefault();
  const fel = $("#oversiktsUtdelningsfel");
  fel.textContent = "";
  const projekt = $("#oversiktsProjekt").value;
  const task = $("#oversiktsTask").value.trim();
  try {
    await hamta(`/api/projects/${encodeURIComponent(projekt)}/dela-ut`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ task, agent: $("#oversiktsAgent").value }),
    });
    $("#oversiktsTask").value = "";
    toast(`${task} har delats ut i ${projekt}.`);
    await laddaAllaOversikt();
  } catch (err) {
    fel.textContent = err.message;
  }
});

setInterval(() => {
  if (vy === "oversikt") laddaAllaOversikt().catch((err) => toast(err.message));
}, 4000);
