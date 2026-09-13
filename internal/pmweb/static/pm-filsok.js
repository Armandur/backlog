let aktivFilsokning = null;

function filsoktraffrad(traff) {
  const lank = document.createElement("a");
  lank.className = "filrad";
  lank.setAttribute("href", `#filer?fil=${encodeURIComponent(traff.fil)}`);

  const radnummer = document.createElement("span");
  radnummer.className = "filikon";
  radnummer.textContent = `Rad ${traff.rad}`;

  const innehall = document.createElement("span");
  innehall.className = "filtraff";
  const fil = document.createElement("strong");
  fil.textContent = traff.fil;
  const rad = document.createElement("span");
  rad.textContent = traff.text || "Tom rad";
  innehall.append(fil, rad);
  lank.append(radnummer, innehall);
  return lank;
}

function visaFilsokresultat(data) {
  const ruta = $("#filsokresultat");
  ruta.textContent = "";
  ruta.hidden = false;
  if (!data.traffar.length) {
    const tom = document.createElement("p");
    tom.className = "tom";
    tom.textContent = "Sökningen gav inga träffar.";
    ruta.append(tom);
  } else {
    data.traffar.forEach((traff) => ruta.append(filsoktraffrad(traff)));
  }
  const tillagg = data.begransad ? " Visningen stannade vid sökningens gräns." : "";
  $("#projektfilsokstatus").textContent = `${data.traffar.length} träffar.${tillagg}`;
}

$("#filsokform").addEventListener("submit", async (event) => {
  event.preventDefault();
  const fraga = $("#projektfilsok").value;
  if (!fraga.trim()) {
    $("#projektfilsokstatus").textContent = "Skriv något att söka efter.";
    $("#filsokresultat").hidden = true;
    return;
  }
  if (aktivFilsokning) aktivFilsokning.abort();
  aktivFilsokning = new AbortController();
  const signal = aktivFilsokning.signal;
  $("#projektfilsokstatus").textContent = "Söker...";
  try {
    const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/filsok?q=${encodeURIComponent(fraga)}`, { signal });
    visaFilsokresultat(data);
  } catch (err) {
    if (err.name !== "AbortError") {
      $("#projektfilsokstatus").textContent = err.message;
      $("#filsokresultat").hidden = true;
    }
  } finally {
    if (aktivFilsokning && aktivFilsokning.signal === signal) aktivFilsokning = null;
  }
});

$("#projektfilsok").addEventListener("input", (event) => {
  if (event.target.value) return;
  if (aktivFilsokning) aktivFilsokning.abort();
  $("#projektfilsokstatus").textContent = "";
  $("#filsokresultat").hidden = true;
  $("#filsokresultat").textContent = "";
});
