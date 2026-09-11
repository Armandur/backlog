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
];

for (const text of fientliga) {
  const rot = nyNod("div");
  rendera(rot, text);
  const noder = alla(rot);
  for (const nod of noder) {
    provaa(`ingen farlig nod för ${JSON.stringify(text)}`, !farliga.includes(nod.taggnamn));
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
