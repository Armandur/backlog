let oppnadFil = null;
let soktraffar = [];
let sokindex = -1;

function filstorlek(byte) {
  return new Intl.NumberFormat("sv-SE").format(byte) + " byte";
}

function filtid(varde) {
  if (!varde) return "Okänd tid";
  return new Intl.DateTimeFormat("sv-SE", { dateStyle: "short", timeStyle: "short" }).format(new Date(varde));
}

function filinnehallsadress(sokvag, nedladdning = false) {
  const grund = `/api/projects/${encodeURIComponent(alias)}/filinnehall?path=${encodeURIComponent(sokvag)}`;
  return nedladdning ? grund + "&download=1" : grund;
}

function filsprak(namn) {
  const andelse = namn.includes(".") ? namn.split(".").pop().toLowerCase() : "";
  return ({ html: "markup", xml: "markup", svg: "markup", css: "css", js: "javascript", mjs: "javascript",
    ts: "typescript", json: "json", go: "go", py: "python", sh: "bash", bash: "bash",
    sql: "sql", md: "markdown", yaml: "yaml", yml: "yaml", toml: "toml", rs: "rust" })[andelse] || "plain";
}

function stangMedia() {
  $("#filbild").removeAttribute("src");
  $("#filvideo").pause();
  $("#filvideo").removeAttribute("src");
  $("#filvideo").load();
}

function visaOppnadFil(data) {
  oppnadFil = data;
  $("#filnamn").textContent = data.namn;
  $("#filmetadata").textContent = `${filstorlek(data.storlek || 0)} · Ändrad ${filtid(data.andrad)}`;
  $("#laddaNerFil").href = filinnehallsadress(data.sokvag, true);
  $("#laddaNerFil").download = data.namn;
  $("#filinnehall").hidden = false;
  $("#filsok").value = "";
  $("#filsokstatus").textContent = "";
  stangMedia();

  const arMedia = Boolean(data.medietyp);
  $("#filverktyg").hidden = arMedia;
  $("#kopieraFil").hidden = arMedia;
  $("#filkod").hidden = arMedia;
  $("#filmedia").hidden = !arMedia;
  if (arMedia) {
    const element = data.medietyp === "bild" ? $("#filbild") : $("#filvideo");
    $("#filbild").hidden = data.medietyp !== "bild";
    $("#filvideo").hidden = data.medietyp !== "video";
    $("#filbild").alt = data.medietyp === "bild" ? `Förhandsvisning av ${data.namn}` : "";
    element.src = filinnehallsadress(data.sokvag);
    return;
  }

  const sprak = filsprak(data.namn);
  const kod = $("#filtext");
  kod.className = `language-${sprak}`;
  if (window.Prism && Prism.languages[sprak]) {
    kod.innerHTML = Prism.highlight(data.innehall, Prism.languages[sprak], sprak);
  } else {
    kod.textContent = data.innehall;
  }
  $("#filradnummer").textContent = data.innehall.split("\n").map((_, index) => index + 1).join("\n");
}

async function kopieraText(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    await navigator.clipboard.writeText(text);
  } else {
    const falt = document.createElement("textarea");
    falt.value = text;
    document.body.append(falt);
    falt.select();
    const lyckades = document.execCommand("copy");
    falt.remove();
    if (!lyckades) throw new Error("Webbläsaren kunde inte kopiera texten.");
  }
  toast("Kopierat.");
}

function markeraSoktraff(start, slut) {
  const rot = $("#filtext");
  const lasare = document.createTreeWalker(rot, NodeFilter.SHOW_TEXT);
  let nod = lasare.nextNode();
  let position = 0;
  let startnod, slutnod, startoffset, slutoffset;
  while (nod) {
    const nasta = position + nod.data.length;
    if (!startnod && start >= position && start <= nasta) {
      startnod = nod;
      startoffset = start - position;
    }
    if (slut >= position && slut <= nasta) {
      slutnod = nod;
      slutoffset = slut - position;
      break;
    }
    position = nasta;
    nod = lasare.nextNode();
  }
  if (!startnod || !slutnod) return;
  const markering = document.createRange();
  markering.setStart(startnod, startoffset);
  markering.setEnd(slutnod, slutoffset);
  const val = window.getSelection();
  val.removeAllRanges();
  val.addRange(markering);
  const rad = oppnadFil.innehall.slice(0, start).split("\n").length - 1;
  const radhöjd = parseFloat(getComputedStyle(rot).lineHeight) || 20;
  $("#filkod").scrollTop = Math.max(0, rad * radhöjd - $("#filkod").clientHeight / 2);
}

function sokIFil(nasta) {
  if (!oppnadFil || oppnadFil.medietyp) return;
  const fraga = $("#filsok").value.toLocaleLowerCase("sv-SE");
  if (!fraga) {
    soktraffar = [];
    sokindex = -1;
    $("#filsokstatus").textContent = "";
    window.getSelection().removeAllRanges();
    return;
  }
  if (!nasta || !soktraffar.length) {
    soktraffar = [];
    const text = oppnadFil.innehall.toLocaleLowerCase("sv-SE");
    for (let pos = text.indexOf(fraga); pos >= 0; pos = text.indexOf(fraga, pos + fraga.length)) soktraffar.push(pos);
    sokindex = 0;
  } else {
    sokindex = (sokindex + 1) % soktraffar.length;
  }
  if (!soktraffar.length) {
    $("#filsokstatus").textContent = "Ingen träff";
    return;
  }
  const start = soktraffar[sokindex];
  $("#filsokstatus").textContent = `${sokindex + 1} av ${soktraffar.length}`;
  markeraSoktraff(start, start + fraga.length);
}

function visaFilsmulor(sokvag) {
  const ruta = $("#filsmulor");
  ruta.textContent = "";
  const delar = sokvag ? sokvag.split("/") : [];
  const rot = document.createElement("button");
  rot.type = "button";
  rot.textContent = "Repo";
  rot.addEventListener("click", () => laddaFiler(""));
  ruta.append(rot);
  delar.forEach((namn, index) => {
    const skilje = document.createElement("span");
    skilje.textContent = "/";
    ruta.append(skilje);
    const knapp = document.createElement("button");
    knapp.type = "button";
    knapp.textContent = namn;
    knapp.addEventListener("click", () => laddaFiler(delar.slice(0, index + 1).join("/")));
    ruta.append(knapp);
  });
}

function filrad(post) {
  const knapp = document.createElement("button");
  knapp.type = "button";
  knapp.className = "filrad";
  knapp.disabled = Boolean(post.symlank);

  const sort = document.createElement("span");
  sort.className = "filikon";
  sort.textContent = post.symlank ? "Länk" : post.katalog ? "Mapp" : "Fil";
  const namn = document.createElement("span");
  namn.textContent = post.namn;
  const metadata = document.createElement("span");
  metadata.className = "filmetadata";
  metadata.textContent = post.katalog ? filtid(post.andrad) : `${filstorlek(post.storlek || 0)} · ${filtid(post.andrad)}`;
  metadata.title = post.andrad || "";
  knapp.append(sort, namn, metadata);
  if (!post.symlank) knapp.addEventListener("click", () => laddaFiler(post.sokvag));
  return knapp;
}

function visaFillista(data) {
  const ruta = $("#fillista");
  ruta.textContent = "";
  if (data.sokvag) {
    const upp = document.createElement("button");
    upp.type = "button";
    upp.className = "filrad";
    const sort = document.createElement("span");
    sort.className = "filikon";
    sort.textContent = "Upp";
    const namn = document.createElement("span");
    namn.textContent = "..";
    upp.append(sort, namn);
    upp.addEventListener("click", () => laddaFiler(data.foralder || ""));
    ruta.append(upp);
  }
  if (!data.poster.length) {
    const tom = document.createElement("p");
    tom.className = "tom";
    tom.textContent = "Katalogen är tom.";
    ruta.append(tom);
  } else {
    data.poster.forEach((post) => ruta.append(filrad(post)));
  }
}


function skapaGitPanel() {
  if ($("#gitpanel")) return;
  const panel = document.createElement("section");
  panel.id = "gitpanel";
  panel.style.marginBottom = "22px";

  const huvud = document.createElement("div");
  huvud.className = "sektionshuvud";
  huvud.style.marginBottom = "10px";
  const rubrik = document.createElement("h2");
  rubrik.textContent = "Ändrat i arbetsträdet";
  const uppdatera = document.createElement("button");
  uppdatera.type = "button";
  uppdatera.className = "btn secondary";
  uppdatera.textContent = "Uppdatera";
  uppdatera.style.marginBottom = "0";
  uppdatera.addEventListener("click", laddaGitAndringar);
  huvud.append(rubrik, uppdatera);

  const fel = document.createElement("p");
  fel.id = "gitfel";
  fel.className = "fel-text";
  fel.setAttribute("role", "alert");
  const vy = document.createElement("div");
  vy.className = "filvy";
  const lista = document.createElement("div");
  lista.id = "gitlista";
  lista.className = "fillista";
  const diff = document.createElement("section");
  diff.id = "gitdiff";
  diff.className = "filinnehall";
  diff.hidden = true;
  const diffhuvud = document.createElement("div");
  diffhuvud.className = "filhuvud";
  const diffnamn = document.createElement("h2");
  diffnamn.id = "gitdiffnamn";
  diffhuvud.append(diffnamn);
  const difftext = document.createElement("pre");
  difftext.id = "gitdifftext";
  diff.append(diffhuvud, difftext);
  vy.append(lista, diff);
  panel.append(huvud, fel, vy);
  $("#filsmulor").before(panel);
}

function gitAndringsrad(andring) {
  const knapp = document.createElement("button");
  knapp.type = "button";
  knapp.className = "filrad";
  const typ = document.createElement("span");
  typ.className = "filikon";
  typ.textContent = andring.typ;
  const sokvag = document.createElement("span");
  sokvag.textContent = andring.sokvag;
  const status = document.createElement("span");
  status.className = "filstorlek";
  status.textContent = andring.status;
  knapp.append(typ, sokvag, status);
  knapp.addEventListener("click", () => laddaGitDiff(andring.sokvag));
  return knapp;
}

async function laddaGitAndringar() {
  skapaGitPanel();
  const lista = $("#gitlista");
  $("#gitfel").textContent = "";
  lista.textContent = "";
  try {
    const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/git-andringar`);
    if (!data.andringar.length) {
      const tom = document.createElement("p");
      tom.className = "tom";
      tom.textContent = "Inga ändringar sedan senaste commit.";
      lista.append(tom);
      return;
    }
    data.andringar.forEach((andring) => lista.append(gitAndringsrad(andring)));
  } catch (err) {
    $("#gitdiff").hidden = true;
    $("#gitfel").textContent = err.message;
  }
}

async function laddaGitDiff(sokvag) {
  $("#gitfel").textContent = "";
  try {
    const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/git-diff?path=${encodeURIComponent(sokvag)}`);
    $("#gitdiffnamn").textContent = data.sokvag;
    $("#gitdifftext").textContent = data.diff || "Filen har ingen textdiff mot HEAD.";
    $("#gitdiff").hidden = false;
  } catch (err) {
    $("#gitdiff").hidden = true;
    $("#gitdifftext").textContent = "";
    $("#gitfel").textContent = err.message;
    toast(err.message);
  }
}

async function laddaFiler(sokvag) {
  if (!sokvag) laddaGitAndringar();
  $("#filfel").textContent = "";
  try {
    const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/filer?path=${encodeURIComponent(sokvag)}`);
    if (data.katalog) {
      visaFilsmulor(data.sokvag);
      visaFillista(data);
      $("#filinnehall").hidden = true;
      $("#filtext").textContent = "";
      oppnadFil = null;
      stangMedia();
      return;
    }
    const delar = data.sokvag.split("/");
    delar.pop();
    visaFilsmulor(delar.join("/"));
    visaOppnadFil(data);
  } catch (err) {
    $("#filinnehall").hidden = true;
    $("#filtext").textContent = "";
    oppnadFil = null;
    stangMedia();
    $("#filfel").textContent = err.message;
    toast(err.message);
  }
}

$("#kopieraFil").addEventListener("click", () => {
  if (oppnadFil && !oppnadFil.medietyp) kopieraText(oppnadFil.innehall).catch((err) => toast(err.message));
});
$("#kopieraSokvag").addEventListener("click", () => {
  if (oppnadFil) kopieraText(oppnadFil.sokvag).catch((err) => toast(err.message));
});
$("#filsok").addEventListener("input", () => sokIFil(false));
$("#filsok").addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    event.preventDefault();
    sokIFil(true);
  }
});
$("#filsokNasta").addEventListener("click", () => sokIFil(true));
$("#radbrytning").addEventListener("click", () => {
  const aktiv = $("#filkod").classList.toggle("radbrytning");
  $("#radbrytning").setAttribute("aria-pressed", String(aktiv));
  $("#radbrytning").textContent = aktiv ? "Behåll långa rader" : "Bryt rader";
});
