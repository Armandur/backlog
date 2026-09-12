function filstorlek(byte) {
  return new Intl.NumberFormat("sv-SE").format(byte) + " byte";
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
  knapp.append(sort, namn);

  if (!post.katalog && !post.symlank) {
    const storlek = document.createElement("span");
    storlek.className = "filstorlek";
    storlek.textContent = filstorlek(post.storlek || 0);
    knapp.append(storlek);
  }
  if (!post.symlank) {
    knapp.addEventListener("click", () => laddaFiler(post.sokvag));
  }
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
      return;
    }
    const delar = data.sokvag.split("/");
    delar.pop();
    visaFilsmulor(delar.join("/"));
    $("#filnamn").textContent = data.namn;
    $("#filstorlek").textContent = filstorlek(data.storlek || 0);
    // Klienten får aldrig tolka filen som HTML eller annan körbar markup.
    $("#filtext").textContent = data.innehall;
    $("#filinnehall").hidden = false;
  } catch (err) {
    $("#filinnehall").hidden = true;
    $("#filtext").textContent = "";
    $("#filfel").textContent = err.message;
    toast(err.message);
  }
}
