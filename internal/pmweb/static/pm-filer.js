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

async function laddaFiler(sokvag) {
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
