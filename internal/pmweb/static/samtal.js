const alias = decodeURIComponent(location.pathname.replace(/^\/pm\/?/, "").split("/")[0] || "");
const trad = document.getElementById("trad");
const status = document.getElementById("status");
const form = document.getElementById("skrivform");
const textfalt = document.getElementById("text");
const aktorfalt = document.getElementById("aktor");
const fragakryss = document.getElementById("fraga");
const knapp = document.getElementById("skicka");

document.getElementById("rubrik").textContent = alias ? `Projektsamtal: ${alias}` : "Projektsamtal";

function tid(ns) {
  const d = new Date(Number(ns) / 1e6);
  return d.toLocaleString("sv-SE", { dateStyle: "short", timeStyle: "short" });
}

function rita(poster) {
  trad.textContent = "";
  if (!poster.length) {
    const li = document.createElement("li");
    li.className = "tom";
    li.textContent = "Tråden är tom. Skriv det första inlägget.";
    trad.append(li);
    return;
  }
  for (const p of poster) {
    const li = document.createElement("li");
    li.className = "inlagg" + (p.actor.kind === "ai" ? " ai" : "");
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
    trad.append(li);
  }
}

async function ladda() {
  if (!alias) {
    status.textContent = "Öppna en tråd med /pm/<projektalias>";
    status.className = "status fel";
    form.hidden = true;
    return;
  }
  try {
    const svar = await fetch(`/api/projects/${encodeURIComponent(alias)}/samtal`);
    const data = await svar.json();
    if (!svar.ok) throw new Error(data.error || "kunde inte hämta tråden");
    rita(data.samtal || []);
    status.textContent = `${(data.samtal || []).length} inlägg`;
    status.className = "status";
  } catch (err) {
    status.textContent = err.message;
    status.className = "status fel";
  }
}

form.addEventListener("submit", async (e) => {
  e.preventDefault();
  const text = textfalt.value.trim();
  if (!text) return;
  knapp.disabled = true;
  status.textContent = fragakryss.checked ? "Frågar agenten..." : "Sparar...";
  status.className = "status";
  try {
    const svar = await fetch(`/api/projects/${encodeURIComponent(alias)}/samtal`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, actor: aktorfalt.value.trim(), fraga: fragakryss.checked }),
    });
    const data = await svar.json();
    if (!svar.ok) throw new Error(data.error || "kunde inte spara inlägget");
    textfalt.value = "";
    await ladda();
  } catch (err) {
    status.textContent = err.message;
    status.className = "status fel";
  } finally {
    knapp.disabled = false;
  }
});

ladda();
