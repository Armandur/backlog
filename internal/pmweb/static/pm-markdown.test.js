// Prov för markdown-renderaren. Körs av go test via internal/pmweb/markdown_test.go.
// Renderaren tar text som agenter skriver, så provet vaktar injektionsytan.
// En liten DOM-attrapp räcker, renderaren använder bara sex DOM-anrop.

const fs = require("fs");
const path = require("path");

function nyNod(taggnamn) {
  const nod = {
    taggnamn,
    tagName: taggnamn.toUpperCase(),
    barn: [],
    attribut: {},
    get textContent() {
      return this.barn.map((b) => (typeof b === "string" ? b : b.textContent)).join("");
    },
    set textContent(varde) {
      this.barn = [String(varde)];
    },
    set className(varde) {
      this.attribut.class = varde;
    },
    set href(varde) {
      this.attribut.href = varde;
    },
    set rel(varde) {
      this.attribut.rel = varde;
    },
    set target(varde) {
      this.attribut.target = varde;
    },
    append(...nya) {
      for (const ny of nya) {
        if (ny && ny.taggnamn === "#fragment") this.barn.push(...ny.barn);
        else this.barn.push(ny);
      }
    },
    replaceChildren(...nya) {
      this.barn = [];
      this.append(...nya);
    },
    // Renderaren får inte bygga med innerHTML. Attrappen tolkar ingen HTML, så
    // en sådan regression hade blivit tyst utan den här vakten.
    set innerHTML(varde) {
      throw new Error("renderaren satte innerHTML: " + String(varde).slice(0, 40));
    },
    setAttribute(namn, varde) {
      this.attribut[namn] = String(varde);
    },
  };
  return nod;
}

global.document = {
  createElement: (namn) => nyNod(namn),
  createTextNode: (text) => String(text),
  createDocumentFragment: () => nyNod("#fragment"),
};
global.window = {};

require(path.join(__dirname, "pm-markdown.js"));
const { rendera } = global.window.PMMarkdown;

function alla(nod, ut = []) {
  for (const barn of nod.barn) {
    if (typeof barn === "string") continue;
    ut.push(barn);
    alla(barn, ut);
  }
  return ut;
}

let fel = 0;
function provaa(namn, villkor) {
  if (!villkor) {
    console.error("FEL: " + namn);
    fel++;
  }
}

const farliga = ["script", "img", "svg", "iframe", "object", "embed", "style", "form", "input"];

const fientliga = [
  "<script>window.pwn = 1;</script>",
  '<img src=x onerror="window.pwn = 1">',
  '<svg onload="window.pwn = 1"></svg>',
  '<iframe src="https://example.com"></iframe>',
  "[ond](javascript:alert(1))",
  "[ond](JaVaScRiPt:alert(1))",
  "[ond]( javascript:alert(1))",
  "[ond](data:text/html,<script>alert(1)</script>)",
  "[ond](vbscript:msgbox)",
  "`<script>alert(1)</script>`",
  "```\n<script>alert(1)</script>\n```",
  "**<img src=x onerror=alert(1)>**",
  "<ScRiPt>window.pwn = 1;</ScRiPt>",
  "[ond](java\tscript:alert(1))",
  "[ond](javascript&#58;alert(1))",
  "<object data=x></object> och <embed src=x> och <form><input></form>",
  "Text med <style>body{display:none}</style> i mitten",
];

for (const text of fientliga) {
  const rot = nyNod("div");
  rendera(rot, text);
  const noder = alla(rot);
  for (const nod of noder) {
    provaa(`ingen farlig nod för ${JSON.stringify(text)}`, !farliga.includes(nod.taggnamn));
    const farligaAttribut = Object.keys(nod.attribut).filter((a) => a.toLowerCase().startsWith("on"));
    provaa(`inga händelseattribut för ${JSON.stringify(text)}`, farligaAttribut.length === 0);
    if (nod.taggnamn === "a") {
      provaa(`bara http eller https i href för ${JSON.stringify(text)}`,
        /^https?:\/\//i.test(nod.attribut.href || ""));
      provaa("länken har rel noopener", (nod.attribut.rel || "").includes("noopener"));
    }
  }
  provaa(`inget kördes för ${JSON.stringify(text)}`, !global.window.pwn);
}

// En riktig länk ska fortfarande bli en länk.
const rot = nyNod("div");
rendera(rot, "Se [sidan](https://example.com) för mer.");
const lankar = alla(rot).filter((n) => n.taggnamn === "a");
provaa("en http-länk blir en länk", lankar.length === 1);
provaa("länkens text följer med", lankar[0] && lankar[0].textContent === "sidan");

// Tabeller renderas som tabell, och en falsk tabell blir text.
const tabellrot = nyNod("div");
rendera(tabellrot, "| Task | Vad |\n|---|---|\n| A | **fet** |\n| B | `kod` |");
const tabelltaggar = alla(tabellrot).map((n) => n.taggnamn);
for (const tagg of ["table", "thead", "tbody", "tr", "th", "td"]) {
  provaa(`tabellen ger ${tagg}`, tabelltaggar.includes(tagg));
}
provaa("cellen renderar inline", tabelltaggar.includes("strong"));

const falskrot = nyNod("div");
rendera(falskrot, "| inte | en tabell |\nutan skiljerad");
provaa("utan skiljerad blir det ingen tabell", !alla(falskrot).some((n) => n.taggnamn === "table"));

const cellrot = nyNod("div");
rendera(cellrot, "| a | b |\n|---|---|\n| <script>window.pwn = 1;</script> | ok |");
provaa("HTML i en cell blir text", !alla(cellrot).some((n) => farliga.includes(n.taggnamn)));
provaa("inget kördes från en cell", !global.window.pwn);

// Citat renderas som blockquote.
const citatrot = nyNod("div");
rendera(citatrot, "> ett citat med **fet** text");
provaa("citat ger blockquote", alla(citatrot).some((n) => n.taggnamn === "blockquote"));

// Vanlig markdown ska fortfarande renderas.
const rot2 = nyNod("div");
rendera(rot2, "# Rubrik\n\n- ett\n- två\n\n**fet** och `kod`\n\n```\nkodblock\n```");
const taggar = alla(rot2).map((n) => n.taggnamn);
for (const tagg of ["h1", "ul", "li", "strong", "code", "pre"]) {
  provaa(`markdown ger ${tagg}`, taggar.includes(tagg));
}

if (fel > 0) {
  console.error(`${fel} prov föll`);
  process.exit(1);
}
console.log("markdown-provet gick igenom");
