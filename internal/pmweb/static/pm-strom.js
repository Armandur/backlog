// Körningens förlopp: händelser via Server-Sent Events. Laddas efter pm.js.
function stangKorningStrom() { if (korningStrom) korningStrom.close(); korningStrom = null; }
function laggHandelse(event) {
  const ruta = $("#forlopp"), foljMed = vidBotten(ruta);
  try {
    const h = JSON.parse(event.data);
    const sort = ["text", "verktyg", "fil", "kommando", "fel"].includes(h.sort) ? h.sort : "text";
    const li = document.createElement("li");
    li.className = `handelse h-${sort}`; li.innerHTML = `<time>${esc(klocka(h.tid))}</time><span class="handelsesort">${esc(sort)}</span><span>${esc(h.text)}</span>`;
    ruta.append(li); if (foljMed) skrollaNed(ruta);
  } catch { toast("PM kunde inte läsa en händelse."); }
}
async function visaForlopp(id) {
  stangKorningStrom();
  const data = await hamta(`/api/korningar/${encodeURIComponent(id)}`);
  $("#forlopp").textContent = ""; $("#forloppstitel").textContent = `${data.korning.task_ref} · ${data.korning.agent}`;
  $("#forloppruta").hidden = false; $("#loggruta").hidden = true;
  byt("korningar", true); $("#forloppruta").scrollIntoView({ block: "nearest" });
  const kallan = new EventSource(`/api/korningar/${encodeURIComponent(id)}/strom`);
  korningStrom = kallan; kallan.onmessage = laggHandelse;
  kallan.addEventListener("slut", (event) => { laggHandelse(event); kallan.close(); if (korningStrom === kallan) korningStrom = null; });
  kallan.onerror = () => { kallan.close(); if (korningStrom === kallan) korningStrom = null; toast("Anslutningen till körningen bröts."); };
}
