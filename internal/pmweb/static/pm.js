const alias = decodeURIComponent(location.pathname.replace(/^\/pm\/?/, "").split("/")[0] || "");
const $ = (s) => document.querySelector(s);
const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
let vy = location.hash.replace("#", "") || (alias ? "projekt" : "projekt-nytt");
let valdKorning = null;
let agenter = [];

function tid(ns) {
  return new Date(Number(ns) / 1e6).toLocaleString("sv-SE", { dateStyle: "short", timeStyle: "short" });
}
function klocka(ns) {
  return new Date(Number(ns) / 1e6).toLocaleTimeString("sv-SE", { hour: "2-digit", minute: "2-digit" });
}
function toast(text) {
  const t = $("#toast");
  t.textContent = text;
  t.classList.add("on");
  clearTimeout(t._h);
  t._h = setTimeout(() => t.classList.remove("on"), 3200);
}
// Nyckeln i JSON är ascii, etiketten i vyn är svensk.
const VANTAR_ETIKETT = { fel: "fel", fraga: "fråga", beslut: "beslut" };

// Säg bara att repot är upptaget när en annan körning faktiskt kör.
function koText(k, alla) {
  if (k.status === "kor") return "startade " + klocka(k.startad_at || k.skapad_at);
  const upptaget = alla.some((a) => a.id !== k.id && a.status === "kor" && a.repo_path === k.repo_path);
  return upptaget ? "köad: repot är upptaget av en annan körning" : "köad, startar strax";
}

function pill(status) {
  return `<span class="pill p-${status}">${status}</span>`;
}
function rad(html) {
  const el = document.createElement("div");
  el.className = "rad-post";
  el.innerHTML = html;
  return el;
}
function fyll(id, poster, bygg, tomtext) {
  const ruta = $(id);
  ruta.textContent = "";
  if (!poster.length) {
    const tom = document.createElement("div");
    tom.className = "tom";
    tom.textContent = tomtext;
    ruta.append(tom);
    return;
  }
  poster.forEach((p) => ruta.append(bygg(p)));
}

async function hamta(url, init) {
  const svar = await fetch(url, init);
  const data = await svar.json().catch(() => ({}));
  if (!svar.ok) throw new Error(data.error || `fel från servern (${svar.status})`);
  return data;
}

// ---------- projektvy ----------
async function laddaOversikt() {
  const o = await hamta(`/api/projects/${encodeURIComponent(alias)}/oversikt`);
  $("#projnamn").textContent = `${o.projekt.name} (${o.projekt.alias})`;
  $("#ptitel").textContent = o.projekt.name;
  $("#pdesc").textContent = [o.projekt.description, o.projekt.repo_path && "Repo: " + o.projekt.repo_path].filter(Boolean).join(" ");

  $("#nPagaende").textContent = o.korningar.length;
  $("#nRun").textContent = o.korningar.length || "";
  fyll("#pagaende", o.korningar, (k) => {
    const el = rad(`<span class="mono ref">${k.task_ref}</span>
      <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
        <span class="meta">${koText(k, o.korningar)}</span></div>
      <div class="act">${pill(k.status)}<button class="btn sm" data-logg="${k.id}">Logg</button></div>`);
    el.classList.add("stripe", "kor");
    return el;
  }, "Inga körningar just nu.");

  const oppna = o.tasks.filter((t) => t.status !== "done");
  // Listan visar alla tasks, så säg vad siffran räknar.
  $("#nTasks").textContent = `${oppna.length} öppna av ${o.tasks.length}`;
  fyll("#tasks", o.tasks, (t) => {
    const kor = o.korningar.find((k) => k.task_ref === t.ref);
    const status = kor ? pill(kor.status) : t.status === "done" ? pill("klar") : t.status === "doing" ? pill("kor") : "";
    const knapp = t.status === "done" || kor ? "" : `<button class="btn sm pri" data-dela="${t.ref}">Dela ut</button>`;
    const sista = t.sista_korning ? ` · senaste körning ${t.sista_korning.status}${t.sista_korning.exit_kod !== undefined ? " (exit " + t.sista_korning.exit_kod + ")" : ""}` : "";
    return rad(`<a class="mono ref" href="/tasks/${encodeURIComponent(t.ref)}" title="Öppna i backlog-UI:t">${t.ref}</a>
      <div class="t"><span class="prio">P${t.prioritet}</span> ${esc(t.titel)}
        <span class="meta">${esc(t.typ)}${t.etiketter.length ? " · " + esc(t.etiketter.join(", ")) : ""}${sista}</span></div>
      <div class="act">${status}${knapp}</div>`);
  }, "Inga tasks i projektet.");

  $("#nVantar").textContent = o.vantar.length;
  fyll("#vantar", o.vantar, (v) => {
    const el = rad(`<span class="pill p-${v.sort}">${VANTAR_ETIKETT[v.sort] || esc(v.sort)}</span>
      <div class="t">${v.ref ? `<span class="mono">${v.ref}</span>` : ""}<span class="meta">${esc(v.text)}</span></div>
      <div class="act">${v.sort === "fraga" ? `<button class="btn sm" data-vy="samtal">Öppna</button>` : v.korning_id ? `<button class="btn sm" data-logg="${v.korning_id}">Logg</button>` : `<button class="btn sm pri" data-dela="${v.ref}">Dela ut</button>`}</div>`);
    el.classList.add("stripe", v.sort);
    return el;
  }, "Inget väntar på dig.");

  $("#nBlockerat").textContent = o.blockerat.length;
  fyll("#blockerat", o.blockerat, (t) => rad(`<a class="mono ref" href="/tasks/${encodeURIComponent(t.ref)}">${t.ref}</a>
    <div class="t">${esc(t.titel)}<span class="meta">${esc(t.skal)}</span></div><div class="act"></div>`), "Inget blockerat.");

  const textrader = Array.from(document.querySelectorAll(".rad-post .t"));
  textrader.forEach((el) => el.setAttribute("title", el.textContent.trim()));
}

$("#taskform").addEventListener("submit", async (e) => {
  e.preventDefault();
  const knapp = $("#skapaTask");
  $("#taskFel").textContent = "";
  knapp.disabled = true;
  try {
    const task = await hamta(`/api/projects/${encodeURIComponent(alias)}/tasks`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        titel: $("#taskTitel").value,
        beskrivning: $("#taskBeskrivning").value,
        typ: $("#taskTyp").value,
        prioritet: Number($("#taskPrioritet").value),
      }),
    });
    $("#taskform").reset();
    await laddaOversikt();
    toast(`${task.ref} har lagts till.`);
  } catch (err) {
    $("#taskFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
});

// ---------- körningar ----------
async function laddaKorningar() {
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/korningar`);
  fyll("#korningar", data.korningar, (k) => rad(`<span class="mono ref">${k.task_ref}</span>
    <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
      <span class="meta">${tid(k.skapad_at)}${k.exit_kod !== undefined ? " · exit " + k.exit_kod : ""}</span></div>
    <div class="act">${pill(k.status)}<button class="btn sm" data-logg="${k.id}">Logg</button></div>`), "Inga körningar än.");
}

async function visaLogg(id) {
  valdKorning = id;
  const data = await hamta(`/api/korningar/${encodeURIComponent(id)}?logg=1`);
  $("#loggruta").hidden = false;
  $("#loggtitel").textContent = `${data.korning.task_ref} · ${data.korning.agent} · ${data.korning.status}`;
  $("#logg").textContent = data.logg || "(ingen logg skriven än)";
  byt("korningar", true);
  $("#loggruta").scrollIntoView({ block: "nearest" });
}

// ---------- samtal ----------
// Tråden ritas om vid varje pollning. Har du skrollat upp ska positionen
// ligga kvar, annars rycks läsningen undan var fjärde sekund.
let samtalLaddat = false;
function vidBotten(ruta) {
  return ruta.scrollHeight - ruta.scrollTop - ruta.clientHeight < 40;
}
function skrollaNed(ruta) {
  ruta.scrollTop = ruta.scrollHeight;
}

async function laddaSamtal() {
  const ruta = $("#trad");
  const foljMed = !samtalLaddat || vidBotten(ruta);
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/samtal`);
  const forePosition = ruta.scrollTop;
  fyll("#trad", data.samtal || [], (p) => {
    const li = document.createElement("li");
    li.className = "inlagg " + (p.actor.kind === "ai" ? "ai" : "jag");
    const meta = document.createElement("div");
    meta.className = "meta";
    const aktor = document.createElement("span");
    aktor.className = "aktor";
    aktor.textContent = `${p.actor.kind}:${p.actor.name}`;
    const nar = document.createElement("span");
    nar.textContent = tid(p.created_at);
    meta.append(aktor, nar);
    const text = document.createElement("p");
    text.className = "text";
    text.textContent = p.text;
    li.append(meta, text);
    return li;
  }, "Tråden är tom. Skriv det första inlägget.");
  if (foljMed) {
    skrollaNed(ruta);
  } else {
    ruta.scrollTop = forePosition;
  }
  samtalLaddat = true;
}

$("#skrivform").addEventListener("submit", async (e) => {
  e.preventDefault();
  const text = $("#text").value.trim();
  if (!text) return;
  const knapp = $("#skicka");
  knapp.disabled = true;
  const fragar = $("#fraga").checked;
  toast(fragar ? "Frågar agenten..." : "Sparar...");
  try {
    await hamta(`/api/projects/${encodeURIComponent(alias)}/samtal`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, actor: $("#aktor").value.trim(), fraga: fragar }),
    });
    $("#text").value = "";
    await laddaSamtal();
    skrollaNed($("#trad"));
  } catch (err) {
    toast(err.message);
  } finally {
    knapp.disabled = false;
  }
});

// ---------- utdelning ----------
let delaRef = null;
function oppnaDela(ref) {
  delaRef = ref;
  $("#dTask").textContent = ref;
  $("#dAgent").innerHTML = `<option value="">regelvald agent</option>` + agenter.map((a) => `<option value="${esc(a)}">${esc(a)}</option>`).join("");
  $("#dNotis").hidden = true;
  $("#drawer").classList.add("on");
  $("#scrim").classList.add("on");
}
function stangDela() {
  $("#drawer").classList.remove("on");
  $("#scrim").classList.remove("on");
}
$("#stang").onclick = $("#dAvbryt").onclick = stangDela;
$("#scrim").onclick = stangDela;
$("#dStarta").onclick = async () => {
  const knapp = $("#dStarta");
  knapp.disabled = true;
  try {
    await hamta(`/api/projects/${encodeURIComponent(alias)}/dela-ut`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ task: delaRef, agent: $("#dAgent").value }),
    });
    stangDela();
    toast(`${delaRef} utdelad. Följ körningen under pågående.`);
    await ladda();
  } catch (err) {
    $("#dNotis").hidden = false;
    $("#dNotis").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
};

// ---------- lägg till projekt ----------
function uppdateraProjektlage() {
  const lage = document.querySelector('input[name="lage"]:checked').value;
  $("#projektSokvagTips").textContent = lage === "nytt"
    ? "PM skapar katalogen, ett Git-repo och README.md."
    : "Katalogen måste redan finnas och innehålla en .git-katalog.";
}

document.querySelectorAll('input[name="lage"]').forEach((val) => val.addEventListener("change", uppdateraProjektlage));
$("#projektform").addEventListener("submit", async (e) => {
  e.preventDefault();
  const knapp = $("#skapaProjekt");
  $("#projektFel").textContent = "";
  $("#projektSvar").hidden = true;
  knapp.disabled = true;
  try {
    const data = await hamta("/api/projekt", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        alias: $("#projektAlias").value,
        namn: $("#projektNamn").value,
        beskrivning: $("#projektBeskrivning").value,
        lage: document.querySelector('input[name="lage"]:checked').value,
        sokvag: $("#projektSokvag").value,
      }),
    });
    $("#projektLank").href = data.lank;
    $("#projektLank").textContent = `Öppna ${data.projekt.name}`;
    $("#projektSvar").hidden = false;
  } catch (err) {
    $("#projektFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
});

// ---------- skal ----------
function byt(ny, behallScroll) {
  if (!alias && ny !== "projekt-nytt" && ny !== "konfig") ny = "projekt-nytt";
  const bytteVy = vy !== ny;
  vy = ny;
  // replaceState i stället för location.hash: annars hoppar webbläsaren till
  // elementet med samma id, t.ex. listan #korningar.
  history.replaceState(null, "", "#" + ny);
  document.querySelectorAll(".rail button").forEach((b) => b.classList.toggle("on", b.dataset.v === vy));
  document.querySelectorAll(".view").forEach((v) => v.classList.toggle("on", v.id === "v-" + vy));
  // En ny vy börjar på sin egen topp, inte där förra vyn var skrollad.
  if (bytteVy && !behallScroll) window.scrollTo(0, 0);
  if (ny === "konfig") laddaKonfig();
}
$("#nav").addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (b) byt(b.dataset.v);
});
document.body.addEventListener("click", (e) => {
  const d = e.target.closest("[data-dela]");
  if (d) return oppnaDela(d.dataset.dela);
  const l = e.target.closest("[data-logg]");
  if (l) return visaLogg(l.dataset.logg);
  const v = e.target.closest("[data-vy]");
  if (v) return byt(v.dataset.vy);
});

async function laddaAgenter() {
  try {
    const data = await hamta("/api/agenter");
    agenter = data.agenter || [];
  } catch {
    agenter = [];
  }
}

async function ladda() {
  if (!alias) {
    $("#projnamn").textContent = "Projekt";
    if (vy === "projekt") byt("projekt-nytt");
    return;
  }
  try {
    await Promise.all([laddaOversikt(), laddaKorningar(), laddaSamtal()]);
  } catch (err) {
    toast(err.message);
  }
}

// Starta först när båda skriptfilerna har körts. Annars saknas konfigvyns
// kod när byt() vill ladda den.
document.addEventListener("DOMContentLoaded", () => {
  byt(vy);
  laddaAgenter().then(ladda);
  // Polling håller pågående körningar aktuella utan omladdning.
  setInterval(ladda, 4000);
});
