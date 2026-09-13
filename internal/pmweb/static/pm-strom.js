// Körningens förlopp: händelser via Server-Sent Events. Laddas efter pm.js.
const handelseLyssnare = [];
function lyssnaPaHandelse(lyssnare) { handelseLyssnare.push(lyssnare); }
function stangKorningStrom() { if (korningStrom) korningStrom.close(); korningStrom = null; }
// Filerna en körning rör nämns om och om igen i strömmen. Set:et gör att varje
// fil listas en gång per körning, se samtalshandelseElement i pm-samtal.js.
let forloppFiler = new Set();
function laggHandelse(event) {
  const ruta = $("#forlopp"), foljMed = vidBotten(ruta);
  try {
    const element = samtalshandelseElement(JSON.parse(event.data), forloppFiler);
    if (element) { ruta.append(element); if (foljMed) skrollaNed(ruta); }
  } catch { toast("PM kunde inte läsa en händelse."); }
  handelseLyssnare.forEach((lyssnare) => lyssnare(event));
}
async function visaForlopp(id) {
  stangKorningStrom();
  forloppFiler = new Set();
  const data = await hamta(`/api/korningar/${encodeURIComponent(id)}`);
  $("#forlopp").textContent = ""; $("#forloppstitel").textContent = `${data.korning.task_ref} · ${data.korning.agent}`;
  $("#forloppruta").hidden = false; $("#loggruta").hidden = true;
  byt("korningar", true); $("#forloppruta").scrollIntoView({ block: "nearest" });
  const kallan = new EventSource(`/api/korningar/${encodeURIComponent(id)}/strom`);
  korningStrom = kallan; kallan.onmessage = laggHandelse;
  kallan.addEventListener("slut", (event) => { laggHandelse(event); kallan.close(); if (korningStrom === kallan) korningStrom = null; });
  kallan.onerror = () => { kallan.close(); if (korningStrom === kallan) korningStrom = null; toast("Anslutningen till körningen bröts."); };
}
