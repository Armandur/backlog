// Uppläsning av agentens svar i samtalet. Laddas före pm-samtal.js.
// Koden bor för sig eftersom talstödet varierar mellan webbläsare och därför
// behöver ett eget prov, se pm-tal.test.js.
(function () {
  const harTalstod = "speechSynthesis" in window && "SpeechSynthesisUtterance" in window;
  let aktivUpplasning = "";
  let upplasningsforsok = 0;

  function uppdateraUpplasningsknappar() {
    document.querySelectorAll("[data-las-upp]").forEach((knapp) => {
      knapp.disabled = knapp.dataset.lasUpp === aktivUpplasning;
    });
    document.querySelectorAll("[data-stoppa-upplasning]").forEach((knapp) => {
      knapp.disabled = knapp.dataset.stoppaUpplasning !== aktivUpplasning;
    });
  }

  function stoppaUpplasning() {
    if (!harTalstod) return;
    upplasningsforsok += 1;
    window.speechSynthesis.cancel();
    aktivUpplasning = "";
    uppdateraUpplasningsknappar();
  }

  function svenskRost() {
    return window.speechSynthesis.getVoices().find((rost) =>
      rost.lang.toLowerCase().startsWith("sv")
    );
  }

  async function vantaPaSvenskRost() {
    const roster = window.speechSynthesis.getVoices();
    const rost = roster.find((val) => val.lang.toLowerCase().startsWith("sv"));
    if (rost || roster.length > 0) return rost;

    await new Promise((klart) => {
      let avslutad = false;
      const avsluta = () => {
        if (avslutad) return;
        avslutad = true;
        window.speechSynthesis.removeEventListener("voiceschanged", avsluta);
        klart();
      };
      window.speechSynthesis.addEventListener("voiceschanged", avsluta);
      setTimeout(avsluta, 1500);
    });
    return svenskRost();
  }

  async function lasUppSvar(post, inlagg) {
    stoppaUpplasning();
    const forsok = upplasningsforsok;
    const rost = await vantaPaSvenskRost();
    if (forsok !== upplasningsforsok) return;
    if (!rost) {
      toast("PM hittar ingen svensk röst i webbläsaren.");
      return;
    }

    const text = inlagg.querySelector(".text").textContent.trim();
    if (!text) return;
    const yttrande = new SpeechSynthesisUtterance(text);
    yttrande.lang = "sv-SE";
    yttrande.voice = rost;
    aktivUpplasning = post.id;
    uppdateraUpplasningsknappar();
    yttrande.onend = () => {
      if (forsok !== upplasningsforsok) return;
      aktivUpplasning = "";
      uppdateraUpplasningsknappar();
    };
    yttrande.onerror = (event) => {
      if (forsok !== upplasningsforsok) return;
      aktivUpplasning = "";
      uppdateraUpplasningsknappar();
      if (event.error !== "canceled" && event.error !== "interrupted") {
        toast("PM kunde inte läsa upp svaret.");
      }
    };
    window.speechSynthesis.speak(yttrande);
  }

  function byggUpplasning(post, inlagg) {
    if (!harTalstod || post.actor.kind !== "ai") return;
    const rad = document.createElement("div");
    rad.className = "upplasning";
    const las = document.createElement("button");
    las.type = "button";
    las.className = "btn sm";
    las.dataset.lasUpp = post.id;
    las.textContent = "Läs upp";
    las.addEventListener("click", () => lasUppSvar(post, inlagg));
    const stopp = document.createElement("button");
    stopp.type = "button";
    stopp.className = "btn sm";
    stopp.dataset.stoppaUpplasning = post.id;
    stopp.textContent = "Stoppa";
    stopp.addEventListener("click", stoppaUpplasning);
    rad.append(las, stopp);
    inlagg.append(rad);
    uppdateraUpplasningsknappar();
  }

  window.PMTal = Object.freeze({ harTalstod, byggUpplasning, stoppaUpplasning });
})();
