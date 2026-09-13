// Körningsvyn hämtar avslutad historik i sidor. Bara pågående pollas.
const KORNINGAR_PER_SIDA = 20;
let korningHistorik = [];
let korningCursor = "";
let korningStatus = "alla";
let aktivaKorningar = [];
let pollningPagar = false;
function byggPagaendeKorning(k) {
  const el = rad(`${taskLank(k.task_ref)}
    <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
      <span class="meta">${koText(k, aktivaKorningar)}${k.modell ? " · modell " + esc(k.modell) : ""}</span></div>
    <div class="act"><span class="korningssnurra" aria-hidden="true"></span>${pill(k.status)}${korningKnappar(k.id)}</div>`);
  return el;
}

function byggHistoriskKorning(k) {
  const exit = k.status === "fel" && k.exit_kod !== undefined ? ` · exit ${k.exit_kod}` : "";
  return rad(`${taskLank(k.task_ref)}
    <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
      <span class="meta">${tid(k.skapad_at)}${exit}</span></div>
    <div class="act">${pill(k.status)}<button class="btn sm" data-kommentarer="${esc(k.task_ref)}">Kommentarer</button>${korningKnappar(k.id)}</div>`);
}

function ritaPagaende() {
  $("#nKorningarPagaende").textContent = aktivaKorningar.length;
  $("#nPagaende").textContent = aktivaKorningar.length;
  $("#nRun").textContent = aktivaKorningar.length || "";
  fyll("#korningarPagaende", aktivaKorningar, byggPagaendeKorning, "Inga körningar just nu.");
  fyll("#pagaende", aktivaKorningar, (k) => {
    const el = rad(`${taskLank(k.task_ref)}
      <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
        <span class="meta">${koText(k, aktivaKorningar)}</span></div>
      <div class="act">${pill(k.status)}${korningKnappar(k.id)}</div>`);
    el.classList.add("stripe", "kor");
    return el;
  }, "Inga körningar just nu.");
}

function ritaHistorik(fler) {
  fyll("#korningarHistorik", korningHistorik, byggHistoriskKorning, "Inga avslutade körningar.");
  $("#visaFlerKorningar").hidden = !fler;
}

async function laddaHistorik(nollstall) {
  if (nollstall) {
    korningHistorik = [];
    korningCursor = "";
  }
  const cursor = korningCursor ? `&innan=${encodeURIComponent(korningCursor)}` : "";
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/korningar?status=${korningStatus}&limit=${KORNINGAR_PER_SIDA}${cursor}`);
  korningHistorik.push(...data.korningar);
  korningCursor = data.nasta_innan || "";
  ritaHistorik(data.fler);
}

async function laddaPagaende(kontrolleraAvslut) {
  const gamla = new Set(aktivaKorningar.map((k) => k.id));
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/korningar?status=pagaende`);
  aktivaKorningar = data.korningar;
  ritaPagaende();
  const avslutad = kontrolleraAvslut && [...gamla].some((id) => !aktivaKorningar.some((k) => k.id === id));
  if (avslutad) {
    await Promise.all([laddaHistorik(true), laddaOversikt()]);
  }
}

async function laddaKorningar() {
  await Promise.all([laddaPagaende(false), laddaHistorik(true)]);
}

async function polla() {
  if (!alias || pollningPagar) return;
  pollningPagar = true;
  try {
    await Promise.all([laddaPagaende(true), laddaSamtal(), laddaTestserver()]);
  } catch (err) {
    toast(err.message);
  } finally {
    pollningPagar = false;
  }
}

document.querySelector(".statusfilter").addEventListener("click", (e) => {
  const knapp = e.target.closest("[data-korning-status]");
  if (!knapp || knapp.dataset.korningStatus === korningStatus) return;
  korningStatus = knapp.dataset.korningStatus;
  document.querySelectorAll("[data-korning-status]").forEach((b) => {
    const vald = b === knapp;
    b.classList.toggle("on", vald);
    b.setAttribute("aria-pressed", String(vald));
  });
  laddaHistorik(true).catch((err) => toast(err.message));
});

$("#visaFlerKorningar").addEventListener("click", () => {
  laddaHistorik(false).catch((err) => toast(err.message));
});

lyssnaPaHandelse((event) => {
  if (event.type === "slut") laddaPagaende(true).catch((err) => toast(err.message));
});
