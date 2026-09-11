(function () {
  "use strict";

  function element(namn, klass) {
    const nod = document.createElement(namn);
    if (klass) nod.className = klass;
    return nod;
  }

  function laggTillText(mal, text) {
    if (text) mal.append(document.createTextNode(text));
  }

  function hittaSlut(text, start, markor) {
    const slut = text.indexOf(markor, start + markor.length);
    return slut > start + markor.length ? slut : -1;
  }

  function renderaInline(mal, varde) {
    const text = String(varde || "");
    let vanlig = "";
    let i = 0;

    function tomVanlig() {
      laggTillText(mal, vanlig);
      vanlig = "";
    }

    while (i < text.length) {
      if (text[i] === "`") {
        const slut = text.indexOf("`", i + 1);
        if (slut > i + 1) {
          tomVanlig();
          const kod = element("code");
          kod.textContent = text.slice(i + 1, slut);
          mal.append(kod);
          i = slut + 1;
          continue;
        }
      }

      if (text[i] === "[") {
        const etikettSlut = text.indexOf("](", i + 1);
        const adressSlut = etikettSlut >= 0 ? text.indexOf(")", etikettSlut + 2) : -1;
        if (etikettSlut > i + 1 && adressSlut > etikettSlut + 2) {
          tomVanlig();
          const etikett = text.slice(i + 1, etikettSlut);
          const adress = text.slice(etikettSlut + 2, adressSlut);
          if (/^https?:\/\//i.test(adress)) {
            const lank = element("a");
            lank.href = adress;
            lank.rel = "noopener noreferrer";
            lank.target = "_blank";
            renderaInline(lank, etikett);
            mal.append(lank);
          } else {
            laggTillText(mal, text.slice(i, adressSlut + 1));
          }
          i = adressSlut + 1;
          continue;
        }
      }

      const markor = text.startsWith("**", i) ? "**" : text.startsWith("__", i) ? "__" : "";
      if (markor) {
        const slut = hittaSlut(text, i, markor);
        if (slut >= 0) {
          tomVanlig();
          const fet = element("strong");
          renderaInline(fet, text.slice(i + 2, slut));
          mal.append(fet);
          i = slut + 2;
          continue;
        }
      }

      if (text[i] === "*" || text[i] === "_") {
        const slut = hittaSlut(text, i, text[i]);
        if (slut >= 0) {
          tomVanlig();
          const kursiv = element("em");
          renderaInline(kursiv, text.slice(i + 1, slut));
          mal.append(kursiv);
          i = slut + 1;
          continue;
        }
      }

      vanlig += text[i];
      i += 1;
    }
    tomVanlig();
  }

  function rendera(mal, varde) {
    const rader = String(varde || "").replace(/\r\n?/g, "\n").split("\n");
    const fragment = document.createDocumentFragment();
    let styckerader = [];
    let lista = null;
    let kodrader = null;

    function tomStycke() {
      if (!styckerader.length) return;
      const stycke = element("p");
      renderaInline(stycke, styckerader.join(" "));
      fragment.append(stycke);
      styckerader = [];
    }

    function tomLista() {
      if (!lista) return;
      fragment.append(lista);
      lista = null;
    }

    function tomBlock() {
      tomStycke();
      tomLista();
    }

    for (const rad of rader) {
      if (kodrader) {
        if (/^\s*```\s*$/.test(rad)) {
          const pre = element("pre");
          const kod = element("code");
          kod.textContent = kodrader.join("\n");
          pre.append(kod);
          fragment.append(pre);
          kodrader = null;
        } else {
          kodrader.push(rad);
        }
        continue;
      }

      if (/^\s*```/.test(rad)) {
        tomBlock();
        kodrader = [];
        continue;
      }

      const rubrik = rad.match(/^ {0,3}(#{1,6})\s+(.+?)\s*#*\s*$/);
      if (rubrik) {
        tomBlock();
        const nod = element(`h${rubrik[1].length}`);
        renderaInline(nod, rubrik[2]);
        fragment.append(nod);
        continue;
      }

      const punkt = rad.match(/^ {0,3}[-+*]\s+(.+)$/);
      const numrerad = rad.match(/^ {0,3}\d+[.)]\s+(.+)$/);
      const listpost = punkt || numrerad;
      if (listpost) {
        tomStycke();
        const sort = numrerad ? "ol" : "ul";
        if (lista && lista.tagName.toLowerCase() !== sort) tomLista();
        if (!lista) lista = element(sort);
        const post = element("li");
        renderaInline(post, listpost[1]);
        lista.append(post);
        continue;
      }

      if (!rad.trim()) {
        tomBlock();
        continue;
      }

      tomLista();
      styckerader.push(rad.trim());
    }

    if (kodrader) {
      const pre = element("pre");
      const kod = element("code");
      kod.textContent = kodrader.join("\n");
      pre.append(kod);
      fragment.append(pre);
    }
    tomBlock();
    mal.replaceChildren(fragment);
  }

  function skapaKort(meta, text) {
    const post = element("article", "kunskapspost");
    const metadata = element("div", "meta");
    const innehall = element("div", "markdown");
    metadata.textContent = meta;
    rendera(innehall, text);
    post.append(metadata, innehall);
    return post;
  }

  window.PMMarkdown = Object.freeze({ rendera, skapaKort });
})();
