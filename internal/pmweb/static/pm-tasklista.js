// Filter, sortering och kolumnläge för projektets tasklista. Laddas efter
// pm.js, som anropar ritaTasklista när översikten hämtats.
//
// Ett riktigt projekt har hundratals tasks. Utan filter drunknar de öppna i
// de klara, och utan sparat läge nollställs valet var fjärde sekund när
// pollningen ritar om vyn.
const TASKFLIKAR = [
  { nyckel: "todo", etikett: "Todo" },
  { nyckel: "doing", etikett: "Doing" },
  { nyckel: "done", etikett: "Done" },
  { nyckel: "alla", etikett: "Alla" },
];
const TASKLISTA_NYCKEL = "pm-tasklista";

let tasklistlage = {
  flik: "todo",
  sok: "",
  typ: "",
  prioritet: "",
  sortering: "prioritet",
  kolumner: false,
};
let senasteTaskdata = null;

try {
  const sparat = JSON.parse(localStorage.getItem(TASKLISTA_NYCKEL) || "null");
  if (sparat) tasklistlage = { ...tasklistlage, ...sparat };
} catch {
  // Ett trasigt sparat läge ska inte stoppa vyn.
}

// Namnet är unikt för den här filen. pm-forslag.js har en egen sparaTasklage
// för valet mellan auto och avancerat, och två globala funktioner med samma
// namn skuggar varandra beroende på skriptordningen.
function sparaTasklistlage() {
  try {
    localStorage.setItem(TASKLISTA_NYCKEL, JSON.stringify(tasklistlage));
  } catch {
    // Privat läge saknar lagring. Filtret gäller ändå den här sidan.
  }
}

function taskStatusnyckel(t) {
  if (t.status === "done") return "done";
  if (t.status === "doing") return "doing";
  return "todo";
}

function taskSeq(t) {
  const siffror = String(t.ref || "").match(/(\d+)/);
  return siffror ? Number(siffror[1]) : 0;
}

// I kolumnläget visas alla tre statusarna sida vid sida. Att då också välja en
// status är ett ickeval, så fliken gäller bara listläget.
function filtreradeTasks(tasks) {
  const sok = tasklistlage.sok.trim().toLowerCase();
  const flikgaller = !tasklistlage.kolumner && tasklistlage.flik !== "alla";
  const valda = tasks.filter((t) => {
    if (flikgaller && taskStatusnyckel(t) !== tasklistlage.flik) return false;
    if (tasklistlage.typ && t.typ !== tasklistlage.typ) return false;
    if (tasklistlage.prioritet && String(t.prioritet) !== tasklistlage.prioritet) return false;
    if (!sok) return true;
    return `${t.ref} ${t.titel}`.toLowerCase().includes(sok);
  });
  const ordning = {
    // Lägst siffra är högst prioritet. Lika prioritet sorteras på referens,
    // annars hoppar raderna omkring mellan pollningarna.
    prioritet: (a, b) => a.prioritet - b.prioritet || taskSeq(b) - taskSeq(a),
    ref: (a, b) => taskSeq(b) - taskSeq(a),
    titel: (a, b) => a.titel.localeCompare(b.titel, "sv"),
  };
  return valda.sort(ordning[tasklistlage.sortering] || ordning.prioritet);
}

function ritaTaskflikar(tasks) {
  const ruta = $("#taskflikar");
  const nytt = document.createDocumentFragment();
  TASKFLIKAR.forEach((flik) => {
    const antal = flik.nyckel === "alla"
      ? tasks.length
      : tasks.filter((t) => taskStatusnyckel(t) === flik.nyckel).length;
    const knapp = document.createElement("button");
    knapp.type = "button";
    knapp.className = "taskflik" + (tasklistlage.flik === flik.nyckel ? " on" : "");
    knapp.dataset.taskflik = flik.nyckel;
    knapp.setAttribute("role", "tab");
    knapp.setAttribute("aria-selected", String(tasklistlage.flik === flik.nyckel));
    knapp.textContent = `${flik.etikett} ${antal}`;
    nytt.append(knapp);
  });
  ruta.replaceChildren(nytt);
}

function fyllTaskval(tasks) {
  const typval = $("#taskTypfilter");
  const typer = [...new Set(tasks.map((t) => t.typ).filter(Boolean))].sort((a, b) => a.localeCompare(b, "sv"));
  // Katalogen kommer ur datan, så en ny tasktyp dyker upp utan kodändring.
  const gammalTyp = typval.value;
  typval.textContent = "";
  typval.append(new Option("Alla typer", ""));
  // Etiketterna bor i pm-forslag.js, som laddas senare. Vid första ritningen
  // finns de, men guarden gör filen oberoende av laddordningen.
  const etiketter = typeof TASKTYP_ETIKETT === "object" ? TASKTYP_ETIKETT : {};
  typer.forEach((typ) => typval.append(new Option(etiketter[typ] || typ, typ)));
  typval.value = typer.includes(gammalTyp) ? gammalTyp : tasklistlage.typ;
  if (typval.value !== tasklistlage.typ) tasklistlage.typ = typval.value;

  const prioval = $("#taskPriofilter");
  const prioriteter = [...new Set(tasks.map((t) => t.prioritet))].sort((a, b) => a - b);
  prioval.textContent = "";
  prioval.append(new Option("Alla prioriteter", ""));
  prioriteter.forEach((p) => prioval.append(new Option(`P${p}`, String(p))));
  prioval.value = tasklistlage.prioritet;
  if (prioval.value !== tasklistlage.prioritet) tasklistlage.prioritet = prioval.value;

  $("#taskSortering").value = tasklistlage.sortering;
  if ($("#taskSok").value !== tasklistlage.sok) $("#taskSok").value = tasklistlage.sok;
}

function ritaTaskkolumner(valda, korningar) {
  const ruta = $("#taskkolumner");
  const nytt = document.createDocumentFragment();
  const kolumner = TASKFLIKAR.filter((f) => f.nyckel !== "alla");
  kolumner.forEach((flik) => {
    const poster = valda.filter((t) => taskStatusnyckel(t) === flik.nyckel);
    const kolumn = document.createElement("section");
    kolumn.className = "taskkolumn";
    const rubrik = document.createElement("h3");
    rubrik.textContent = `${flik.etikett} ${poster.length}`;
    kolumn.append(rubrik);
    if (!poster.length) {
      const tom = document.createElement("div");
      tom.className = "tom";
      tom.textContent = "Inget här.";
      kolumn.append(tom);
    }
    poster.forEach((t) => kolumn.append(byggTaskrad(t, korningar)));
    nytt.append(kolumn);
  });
  ruta.replaceChildren(nytt);
}

// ritaTasklista anropas av pm.js varje gång översikten hämtats, alltså var
// fjärde sekund. Den måste därför vara billig och lämna filtret i fred.
function ritaTasklista(o) {
  senasteTaskdata = o;
  const tasks = o.tasks || [];
  ritaTaskflikar(tasks);
  fyllTaskval(tasks);
  const valda = filtreradeTasks(tasks);
  const oppna = tasks.filter((t) => t.status !== "done");
  $("#nTasks").textContent = `${oppna.length} öppna av ${tasks.length}`;
  $("#taskfiltertext").textContent = valda.length === tasks.length
    ? ""
    : `Visar ${valda.length} av ${tasks.length} tasks.`;
  $("#taskVylage").textContent = tasklistlage.kolumner ? "Lista" : "Kolumner";
  $("#taskVylage").setAttribute("aria-pressed", String(tasklistlage.kolumner));
  $("#taskflikar").hidden = tasklistlage.kolumner;
  $("#tasks").hidden = tasklistlage.kolumner;
  $("#taskkolumner").hidden = !tasklistlage.kolumner;
  if (tasklistlage.kolumner) {
    ritaTaskkolumner(valda, o.korningar || []);
    return;
  }
  fyll("#tasks", valda, (t) => byggTaskrad(t, o.korningar || []), "Inga tasks matchar filtret.");
  const textrader = Array.from(document.querySelectorAll("#tasks .rad-post .t"));
  textrader.forEach((el) => el.setAttribute("title", el.textContent.trim()));
}

// Ett filterbyte ändrar listans höjd. Byter man från 121 klara till 21 öppna
// krymper sidan, och då hamnar man någonstans ovanför listan man just valde.
// Verktygsraden dras därför in i vyn, men bara när den faktiskt hamnat utanför.
function rittaOmTasklistan() {
  if (!senasteTaskdata) return;
  ritaTasklista(senasteTaskdata);
  $("#taskverktyg").scrollIntoView({ block: "nearest" });
}

$("#taskflikar").addEventListener("click", (event) => {
  const knapp = event.target.closest("[data-taskflik]");
  if (!knapp) return;
  tasklistlage.flik = knapp.dataset.taskflik;
  sparaTasklistlage();
  rittaOmTasklistan();
});

$("#taskSok").addEventListener("input", (event) => {
  tasklistlage.sok = event.target.value;
  sparaTasklistlage();
  rittaOmTasklistan();
});

$("#taskTypfilter").addEventListener("change", (event) => {
  tasklistlage.typ = event.target.value;
  sparaTasklistlage();
  rittaOmTasklistan();
});

$("#taskPriofilter").addEventListener("change", (event) => {
  tasklistlage.prioritet = event.target.value;
  sparaTasklistlage();
  rittaOmTasklistan();
});

$("#taskSortering").addEventListener("change", (event) => {
  tasklistlage.sortering = event.target.value;
  sparaTasklistlage();
  rittaOmTasklistan();
});

$("#taskVylage").addEventListener("click", () => {
  tasklistlage.kolumner = !tasklistlage.kolumner;
  sparaTasklistlage();
  rittaOmTasklistan();
});

// Filtreringen och sorteringen är ren logik, och provas därför utan DOM. Se
// pm-tasklista.test.js.
if (typeof module === "object") {
  module.exports = {
    filtreradeTasks,
    taskStatusnyckel,
    satt: (nytt) => Object.assign(tasklistlage, nytt),
  };
}
