const alias = decodeURIComponent(location.pathname.replace(/^\/pm\/?/, "").split("/")[0] || "");
const $ = (s) => document.querySelector(s);
const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
let vy = location.hash.replace("#", "") || (alias ? "projekt" : "projekt-nytt");
let korningStrom = null;
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
// Statusvärdena i databasen är ASCII, etiketten i vyn är svensk.
const STATUS_ETIKETT = { koad: "Köad", kor: "Kör", klar: "Klar", fel: "Fel" };
function pill(status) { return `<span class="pill p-${esc(status)}">${esc(STATUS_ETIKETT[status] || status)}</span>`; }
// Referensen går till backlog-UI:t, som i projektvyn. Kommentarsknappen är
// vägen till agentens rapport utan att lämna PM.
function taskLank(ref) {
  return `<a class="mono ref" href="/tasks/${encodeURIComponent(ref)}" title="Öppna i backlog-UI:t">${esc(ref)}</a>`;
}
function korningKnappar(id) { return `<button class="btn sm pri" data-forlopp="${id}">Förlopp</button><button class="btn sm" data-logg="${id}">Logg</button>`; }
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
async function laddaOversikt() {
  const o = await hamta(`/api/projects/${encodeURIComponent(alias)}/oversikt`);
  $("#ptitel").textContent = o.projekt.name;
  $("#pdesc").textContent = [o.projekt.description, o.projekt.repo_path && "Repo: " + o.projekt.repo_path].filter(Boolean).join(" ");
  visaProjektstad(o.projekt);
  $("#nPagaende").textContent = o.korningar.length;
  $("#nRun").textContent = o.korningar.length || "";
  fyll("#pagaende", o.korningar, (k) => {
    const el = rad(`${taskLank(k.task_ref)}
      <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
        <span class="meta">${koText(k, o.korningar)}</span></div>
      <div class="act">${pill(k.status)}${korningKnappar(k.id)}</div>`);
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
    const kommentarknapp = `<button class="btn sm" data-kommentarer="${esc(t.ref)}">Kommentarer</button>`;
    const redigeraknapp = `<button class="btn sm" data-redigera="${esc(t.ref)}">Redigera</button>`;
    const sista = t.sista_korning ? ` · senaste körning ${t.sista_korning.status}${t.sista_korning.exit_kod !== undefined ? " (exit " + t.sista_korning.exit_kod + ")" : ""}` : "";
    return rad(`<a class="mono ref" href="/tasks/${encodeURIComponent(t.ref)}" title="Öppna i backlog-UI:t">${t.ref}</a>
      <div class="t"><span class="prio">P${t.prioritet}</span> ${esc(t.titel)}
        <span class="meta">${esc(t.typ)}${t.etiketter.length ? " · " + esc(t.etiketter.join(", ")) : ""}${sista}</span></div>
      <div class="act">${status}${redigeraknapp}${kommentarknapp}<button class="btn sm" data-taskforslag="klassning" data-taskref="${esc(t.ref)}">Klassa</button><button class="btn sm" data-taskforslag="berikning" data-taskref="${esc(t.ref)}">Berika</button>${knapp}</div>`);
  }, "Inga tasks i projektet.");
  $("#nVantar").textContent = o.vantar.length;
  fyll("#vantar", o.vantar, (v) => {
    const el = rad(`<span class="pill p-${v.sort}">${VANTAR_ETIKETT[v.sort] || esc(v.sort)}</span>
      <div class="t">${v.ref ? `<span class="mono">${v.ref}</span>` : ""}<span class="meta">${esc(v.text)}</span></div>
      <div class="act">${v.sort === "fraga" ? `<button class="btn sm" data-vy="samtal">Öppna</button>` : v.korning_id ? korningKnappar(v.korning_id) : `<button class="btn sm pri" data-dela="${v.ref}">Dela ut</button>`}</div>`);
    el.classList.add("stripe", v.sort);
    return el;
  }, "Inget väntar på dig.");
  $("#nBlockerat").textContent = o.blockerat.length;
  fyll("#blockerat", o.blockerat, (t) => rad(`<a class="mono ref" href="/tasks/${encodeURIComponent(t.ref)}">${t.ref}</a>
    <div class="t">${esc(t.titel)}<span class="meta">${esc(t.skal)}</span></div>
    <div class="act"><button class="btn sm" data-kommentarer="${esc(t.ref)}">Kommentarer</button></div>`), "Inget blockerat.");
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
async function laddaKorningar() {
  const data = await hamta(`/api/projects/${encodeURIComponent(alias)}/korningar`);
  fyll("#korningar", data.korningar, (k) => rad(`${taskLank(k.task_ref)}
    <div class="t"><span class="mono">${esc(k.agent)}</span> ${esc(k.motivering)}
      <span class="meta">${tid(k.skapad_at)}${k.modell ? " · modell " + esc(k.modell) : ""}${k.exit_kod !== undefined ? " · exit " + k.exit_kod : ""}</span></div>
    <div class="act">${pill(k.status)}<button class="btn sm" data-kommentarer="${esc(k.task_ref)}">Kommentarer</button>${korningKnappar(k.id)}</div>`), "Inga körningar än.");
}
async function visaLogg(id) {
  stangKorningStrom();
  const data = await hamta(`/api/korningar/${encodeURIComponent(id)}?logg=1`);
  $("#forloppruta").hidden = true; $("#loggruta").hidden = false;
  $("#loggtitel").textContent = `${data.korning.task_ref} · ${data.korning.agent} · ${data.korning.status}`;
  $("#logg").textContent = data.logg || "(ingen logg skriven än)";
  byt("korningar", true); $("#loggruta").scrollIntoView({ block: "nearest" });
}
// Tråden ritas om vid varje pollning. Har du skrollat upp ska positionen
// ligga kvar, annars rycks läsningen undan var fjärde sekund.
let samtalLaddat = false;
function vidBotten(ruta) { return ruta.scrollHeight - ruta.scrollTop - ruta.clientHeight < 40; }
function skrollaNed(ruta) { ruta.scrollTop = ruta.scrollHeight; }
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
async function visaKommentarer(ref) {
  $("#kommentarTitel").textContent = `${ref} · kommentarer`;
  $("#kommentarer").innerHTML = '<div class="tom">Läser kommentarer...</div>';
  stangDela();
  $("#kommentarsdrawer").classList.add("on");
  $("#scrim").classList.add("on");
  try {
    const data = await hamta(`/api/tasks/${encodeURIComponent(ref)}/kommentarer`);
    fyll("#kommentarer", data.kommentarer || [], (c) =>
      PMMarkdown.skapaKort(`${c.actor.kind}:${c.actor.name} · ${tid(c.created_at)}`, c.body), "Tasken saknar kommentarer.");
  } catch (err) {
    $("#kommentarer").textContent = err.message;
  }
}
function stangKommentarer() {
  $("#kommentarsdrawer").classList.remove("on");
  if (!$("#drawer").classList.contains("on")) $("#scrim").classList.remove("on");
}
$("#kommentarStang").onclick = stangKommentarer;
async function visaDoc(id) {
  const data = await hamta(`/api/docs/${encodeURIComponent(id)}`);
  const doc = data.doc;
  $("#dokument").hidden = false;
  $("#dokumentTitel").textContent = doc.title;
  $("#dokumentMeta").textContent = `${doc.version.actor.kind}:${doc.version.actor.name} · version ${doc.current_version} · ${tid(doc.updated_at)}`;
  PMMarkdown.rendera($("#dokumentText"), doc.version.body);
  $("#dokument").scrollIntoView({ block: "nearest" });
}
async function laddaKunskap() {
  const [docs, minne] = await Promise.all([
    hamta(`/api/projects/${encodeURIComponent(alias)}/docs`),
    hamta(`/api/projects/${encodeURIComponent(alias)}/minne`),
  ]);
  fyll("#docs", docs.docs || [], (d) => rad(`<div class="t"><strong>${esc(d.title)}</strong>
    <span class="meta">version ${d.current_version} · ${tid(d.updated_at)}</span></div>
    <div class="act"><button class="btn sm" data-doc="${esc(d.id)}">Öppna</button></div>`), "Projektet saknar docs.");
  fyll("#minne", minne.minne || [], (m) =>
    PMMarkdown.skapaKort(`${m.actor.kind}:${m.actor.name} · ${tid(m.created_at)}${m.tags ? " · " + m.tags : ""}`, m.body), "Projektminnet är tomt.");
}
let delaRef = null;
function oppnaDela(ref) {
  // Panelerna ligger på samma plats, så bara en i taget får vara öppen.
  stangKommentarer();
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
$("#scrim").onclick = () => { stangDela(); stangKommentarer(); };
$("#dStarta").onclick = async () => {
  const knapp = $("#dStarta");
  knapp.disabled = true;
  try {
    await hamta(`/api/projects/${encodeURIComponent(alias)}/dela-ut`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        task: delaRef,
        agent: $("#dAgent").value,
        modell: $("#dModell").value,
        anstrangning: $("#dAnstrangning").value,
      }),
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
        startkommando: $("#projektStart").value,
      }),
    });
    $("#projektLank").href = data.lank;
    $("#projektLank").textContent = `Öppna ${data.projekt.name}`;
    $("#projektSvar").hidden = false;
    if (data.varning) toast(data.varning);
  } catch (err) {
    $("#projektFel").textContent = err.message;
  } finally {
    knapp.disabled = false;
  }
});
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
  if (ny !== "korningar") stangKorningStrom();
  if (ny === "konfig") laddaKonfig();
  if (ny === "kunskap") laddaKunskap().catch((err) => toast(err.message));
  if (ny === "filer") laddaFiler("").catch((err) => toast(err.message));
}
$("#nav").addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (b) byt(b.dataset.v);
});
document.body.addEventListener("click", (e) => {
  const d = e.target.closest("[data-dela]");
  if (d) return oppnaDela(d.dataset.dela);
  const f = e.target.closest("[data-forlopp]");
  if (f) return visaForlopp(f.dataset.forlopp);
  const l = e.target.closest("[data-logg]");
  if (l) return visaLogg(l.dataset.logg);
  const k = e.target.closest("[data-kommentarer]");
  if (k) return visaKommentarer(k.dataset.kommentarer);
  const docKnapp = e.target.closest("[data-doc]");
  if (docKnapp) return visaDoc(docKnapp.dataset.doc).catch((err) => toast(err.message));
  const v = e.target.closest("[data-vy]");
  if (v) return byt(v.dataset.vy);
});
async function laddaAgenter() {
  try {
    const data = await hamta("/api/agenter");
    agenter = data.agenter || [];
    $("#fragatips").textContent = data.forval
      ? `Med bock skickar PM inlägget till ${data.forval}, som svarar i tråden. Agenten ser öppna tasks, projektminnet och de senaste inläggen. Utan bock sparar PM bara inlägget.`
      : "Utan bock sparar PM bara inlägget i tråden.";
  } catch {
    agenter = [];
  }
}
async function laddaProjektval() {
  const valjare = $("#projektval");
  try {
    // Arkiverade projekt följer med, annars går de inte att hitta tillbaka till.
    const data = await hamta("/api/projects?include_archived=true");
    const projekt = (data.projects || []).slice().sort((a, b) => a.name.localeCompare(b.name, "sv"));
    valjare.innerHTML = `<option value="">Välj projekt</option>` +
      projekt.map((p) => `<option value="${esc(p.alias)}"${p.alias === alias ? " selected" : ""}>${esc(p.name)} (${esc(p.alias)})${p.archived_at ? " - arkiverat" : ""}</option>`).join("");
  } catch {
    valjare.innerHTML = `<option value="">Kunde inte läsa projekten</option>`;
  }
}
$("#projektval").addEventListener("change", (e) => {
  if (e.target.value) window.location.href = `/pm/${encodeURIComponent(e.target.value)}${location.hash}`;
});
async function ladda() {
  if (!alias) {
    if (vy === "projekt") byt("projekt-nytt");
    return;
  }
  try {
    await Promise.all([laddaOversikt(), laddaKorningar(), laddaSamtal(), laddaTestserver()]);
  } catch (err) {
    toast(err.message);
  }
}
// Starta efter båda skriptfilerna. Annars saknas konfigkoden när byt() laddar vyn.
document.addEventListener("DOMContentLoaded", () => {
  byt(vy);
  // Väljaren listar projekten. Annars måste användaren känna till adressen
  // och skriva den själv.
  laddaProjektval();
  laddaAgenter().then(ladda);
  // Polling håller pågående körningar aktuella utan omladdning.
  setInterval(ladda, 4000);
});
