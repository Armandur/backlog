// Prov för tasklistans filter och sortering. Körs av go test via
// internal/pmweb/tasklista_test.go. Ett riktigt projekt har hundratals tasks,
// så urvalet måste vara rätt, inte ungefär rätt.

const path = require("path");

// Filen kopplar lyssnare vid inläsningen. Attrappen räcker, provet rör bara
// den rena logiken.
function nyNod() {
  return { addEventListener() {}, append() {}, textContent: "", value: "", dataset: {}, setAttribute() {}, hidden: false };
}
global.$ = () => nyNod();
global.document = { createElement: nyNod, querySelectorAll: () => [] };
global.localStorage = {
  data: {},
  getItem(nyckel) { return this.data[nyckel] || null; },
  setItem(nyckel, varde) { this.data[nyckel] = varde; },
};

const { filtreradeTasks, taskStatusnyckel, satt } = require(path.join(__dirname, "pm-tasklista.js"));

let fel = 0;
function provaa(namn, villkor) {
  if (!villkor) {
    console.error("FEL: " + namn);
    fel++;
  }
}

const tasks = [
  { ref: "TASK-1", titel: "Fixa chatten", typ: "bug", prioritet: 2, status: "todo" },
  { ref: "TASK-2", titel: "Bygg lobbyn", typ: "feature", prioritet: 1, status: "todo" },
  { ref: "TASK-10", titel: "Avatarbomb", typ: "vulnerability", prioritet: 1, status: "done" },
  { ref: "TASK-11", titel: "Städa loggen", typ: "chore", prioritet: 4, status: "doing" },
];

// Statusen i databasen har tre värden, och allt som inte är done eller doing
// räknas som todo. Annars skulle en task med okänd status försvinna ur alla flikar.
provaa("okänd status hamnar i todo", taskStatusnyckel({ status: "blocked" }) === "todo");
provaa("done känns igen", taskStatusnyckel({ status: "done" }) === "done");

satt({ flik: "todo", sok: "", typ: "", prioritet: "", sortering: "prioritet" });
let valda = filtreradeTasks(tasks);
provaa("todo-fliken tar bara öppna", valda.length === 2 && valda.every((t) => t.status === "todo"));
provaa("lägst siffra är högst prioritet", valda[0].ref === "TASK-2");

satt({ flik: "alla" });
provaa("alla-fliken tar allt", filtreradeTasks(tasks).length === 4);

satt({ flik: "done" });
valda = filtreradeTasks(tasks);
provaa("done-fliken tar de klara", valda.length === 1 && valda[0].ref === "TASK-10");

// Sökningen träffar både titel och referens, och bryr sig inte om versaler.
satt({ flik: "alla", sok: "CHATT" });
provaa("sökningen är skiftlägesokänslig", filtreradeTasks(tasks).length === 1);
satt({ sok: "task-10" });
provaa("sökningen träffar referensen", filtreradeTasks(tasks)[0].ref === "TASK-10");
satt({ sok: "   " });
provaa("bara blanksteg filtrerar inget", filtreradeTasks(tasks).length === 4);

satt({ sok: "", typ: "bug" });
provaa("typfiltret träffar", filtreradeTasks(tasks).length === 1);
satt({ typ: "", prioritet: "1" });
valda = filtreradeTasks(tasks);
provaa("prioritetsfiltret träffar", valda.length === 2 && valda.every((t) => t.prioritet === 1));

// Filtren gäller samtidigt, inte ett i taget.
satt({ typ: "feature", prioritet: "1", flik: "todo" });
provaa("filtren kombineras", filtreradeTasks(tasks).length === 1);

// Lika prioritet måste ge stabil ordning, annars hoppar raderna omkring var
// fjärde sekund när pollningen ritar om listan.
satt({ flik: "alla", typ: "", prioritet: "", sortering: "prioritet" });
valda = filtreradeTasks(tasks);
provaa("nyast först vid lika prioritet", valda[0].ref === "TASK-10" && valda[1].ref === "TASK-2");

satt({ sortering: "ref" });
provaa("referenssortering räknar siffran, inte texten", filtreradeTasks(tasks)[0].ref === "TASK-11");

satt({ sortering: "titel" });
provaa("titelsortering följer svensk ordning", filtreradeTasks(tasks)[0].titel === "Avatarbomb");

// Kolumnläget visar alla tre statusarna sida vid sida. Då är det ett ickeval
// att också filtrera på en av dem, så fliken ska inte gälla.
satt({ flik: "todo", sok: "", typ: "", prioritet: "", sortering: "prioritet", kolumner: true });
provaa("fliken gäller inte i kolumnläget", filtreradeTasks(tasks).length === 4);
satt({ typ: "bug" });
provaa("övriga filter gäller ändå i kolumnläget", filtreradeTasks(tasks).length === 1);
satt({ typ: "", kolumner: false });
provaa("fliken gäller igen i listläget", filtreradeTasks(tasks).length === 2);

if (fel > 0) {
  console.error(`${fel} prov föll`);
  process.exit(1);
}
console.log("tasklistprovet gick igenom");
