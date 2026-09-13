// Prov för uppläsningen i samtalet. Körs av go test via internal/pmweb/tal_test.go.
// Själva ljudet går inte att prova här, men knapparnas villkor, stoppanropet
// och beskedet om saknad svensk röst gör det.

const path = require("path");

function nyNod(taggnamn) {
  return {
    taggnamn,
    barn: [],
    dataset: {},
    disabled: false,
    lyssnare: {},
    className: "",
    textContent: "",
    type: "",
    append(...nya) {
      this.barn.push(...nya);
    },
    addEventListener(namn, fn) {
      this.lyssnare[namn] = fn;
    },
    querySelector() {
      return { textContent: "Ett svar från agenten." };
    },
  };
}

const noder = [];
global.document = {
  createElement(namn) {
    const nod = nyNod(namn);
    noder.push(nod);
    return nod;
  },
  querySelectorAll(valjare) {
    const nyckel = valjare === "[data-las-upp]" ? "lasUpp" : "stoppaUpplasning";
    return noder.filter((n) => n.dataset[nyckel] !== undefined);
  },
};

let toastat = [];
global.toast = (text) => toastat.push(text);

const talade = [];
let roster = [];
global.SpeechSynthesisUtterance = function (text) {
  this.text = text;
};
global.window = {
  speechSynthesis: {
    avbrutna: 0,
    getVoices: () => roster,
    cancel() {
      this.avbrutna += 1;
    },
    speak: (yttrande) => talade.push(yttrande),
    addEventListener() {},
    removeEventListener() {},
  },
  SpeechSynthesisUtterance: global.SpeechSynthesisUtterance,
};
global.window.document = global.document;

require(path.join(__dirname, "pm-tal.js"));
const PMTal = global.window.PMTal;

let fel = 0;
function provaa(namn, villkor) {
  if (!villkor) {
    console.error("FEL: " + namn);
    fel++;
  }
}

provaa("talstöd hittas när webbläsaren har det", PMTal.harTalstod === true);

// Knapparna byggs bara på agentens inlägg.
const mittInlagg = nyNod("article");
PMTal.byggUpplasning({ id: "p1", actor: { kind: "human" } }, mittInlagg);
provaa("inga knappar på mitt eget inlägg", mittInlagg.barn.length === 0);

const agentinlagg = nyNod("article");
const post = { id: "p2", actor: { kind: "ai" } };
PMTal.byggUpplasning(post, agentinlagg);
provaa("agentens inlägg får en rad", agentinlagg.barn.length === 1);
const rad = agentinlagg.barn[0];
provaa("raden har två knappar", rad.barn.length === 2);
const [las, stopp] = rad.barn;
provaa("läsknappen bär inläggets id", las.dataset.lasUpp === "p2");
provaa("stoppknappen bär inläggets id", stopp.dataset.stoppaUpplasning === "p2");
provaa("stoppknappen börjar avstängd", stopp.disabled === true);
provaa("läsknappen börjar aktiv", las.disabled === false);

async function kor() {
  // Utan svensk röst blir det besked i stället för tal.
  roster = [{ lang: "en-US" }];
  toastat = [];
  await las.lyssnare.click();
  provaa("besked när svensk röst saknas", toastat.some((t) => t.includes("svensk röst")));
  provaa("inget yttrande utan svensk röst", talade.length === 0);

  // Med svensk röst läses svaret upp på svenska.
  roster = [{ lang: "en-US" }, { lang: "sv-SE", name: "Alva" }];
  toastat = [];
  await las.lyssnare.click();
  provaa("ett yttrande skickades", talade.length === 1);
  provaa("yttrandet är på svenska", talade[0].lang === "sv-SE");
  provaa("yttrandet fick den svenska rösten", talade[0].voice.name === "Alva");
  provaa("texten kom från inlägget", talade[0].text === "Ett svar från agenten.");
  provaa("läsknappen stängs av under uppläsningen", las.disabled === true);
  provaa("stoppknappen blir klickbar", stopp.disabled === false);

  // Stoppet avbryter och släpper knapparna.
  const fore = global.window.speechSynthesis.avbrutna;
  stopp.lyssnare.click();
  provaa("stoppet avbröt talet", global.window.speechSynthesis.avbrutna === fore + 1);
  provaa("läsknappen blir klickbar igen", las.disabled === false);
  provaa("stoppknappen stängs av igen", stopp.disabled === true);

  // Ett svar som kommer efter stoppet får inte börja tala.
  roster = [{ lang: "sv-SE", name: "Alva" }];
  const senkommen = las.lyssnare.click();
  PMTal.stoppaUpplasning();
  await senkommen;
  provaa("stoppet hinner före en påbörjad uppläsning", talade.length === 1);

  if (fel > 0) {
    console.error(`${fel} prov föll`);
    process.exit(1);
  }
  console.log("talprovet gick igenom");
}

kor();
