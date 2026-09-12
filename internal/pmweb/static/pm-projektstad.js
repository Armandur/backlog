// Arkivera, återställ och ta bort projekt. Laddas efter pm.js.

// visaProjektstad ställer knapparna efter om projektet är arkiverat.
function visaProjektstad(projekt) {
  const arkiverat = Boolean(projekt && projekt.archived_at);
  $("#arkiveraProjekt").hidden = arkiverat;
  $("#aterstallProjekt").hidden = !arkiverat;
  $("#taBortProjekt").hidden = !arkiverat;
  $("#projektstadText").textContent = arkiverat
    ? "Projektet är arkiverat. Det syns bara för den som letar upp det."
    : "";
}

async function projektstadAnrop(vag, metod, lyckades) {
  try {
    await hamta(vag, { method: metod });
    toast(lyckades);
    return true;
  } catch (err) {
    toast(err.message);
    return false;
  }
}

$("#arkiveraProjekt").addEventListener("click", async () => {
  if (!confirm("Arkivera projektet? Inget försvinner, men projektet lämnar listan.")) return;
  if (await projektstadAnrop(`/api/projekt/${encodeURIComponent(alias)}/arkivera`, "POST", "Projektet är arkiverat.")) {
    await laddaOversikt();
    await laddaProjektval();
  }
});

$("#aterstallProjekt").addEventListener("click", async () => {
  if (await projektstadAnrop(`/api/projekt/${encodeURIComponent(alias)}/aterstall`, "POST", "Projektet är tillbaka.")) {
    await laddaOversikt();
    await laddaProjektval();
  }
});

$("#taBortProjekt").addEventListener("click", async () => {
  const svar = prompt(
    `Ta bort projektet ${alias} helt? PM raderar dess tasks, planer, docs, samtal, körningar och minne. Koden på disken rörs inte.\n\nSkriv projektets alias för att bekräfta:`);
  if (svar === null) return;
  if (svar.trim() !== alias) {
    toast("Aliaset stämde inte, så PM tog inte bort något.");
    return;
  }
  try {
    const data = await hamta(`/api/projekt/${encodeURIComponent(alias)}`, { method: "DELETE" });
    if (data.varning) toast(data.varning);
    window.location.href = "/pm/";
  } catch (err) {
    toast(err.message);
  }
});
