#!/usr/bin/env python3
"""Kör klarspråkslintern rådgivande på projektets svenska texter."""

from __future__ import annotations

import ast
import importlib.util
import os
import re
from dataclasses import dataclass
from html.parser import HTMLParser
from pathlib import Path


ROT = Path(__file__).resolve().parents[1]
STANDARD_LINTER = Path.home() / "workspace" / "klarspråk" / "klarsprak_lint.py"
SVENSKA_ORD = {
    "aldrig", "alla", "ange", "använd", "att", "av", "behöver", "den",
    "det", "din", "du", "eller", "en", "ett", "fel", "finns", "för",
    "från", "har", "här", "inte", "kan", "kunde", "med", "måste", "och",
    "om", "på", "saknas", "ska", "som", "till", "utan", "var", "välj",
}
ATTRIBUT = {"alt", "aria-label", "placeholder", "title"}
KATEGORIER = (
    "lang_mening", "passiv", "nominalisering", "forbjudet_ord",
    "marknadsord", "hedge_fras", "semikolon", "langt_stycke",
)


@dataclass(frozen=True)
class Textbit:
    fil: Path
    rad: int
    slag: str
    text: str


def _ar_svenska(text: str) -> bool:
    ord_ = {ord_.lower() for ord_ in re.findall(r"[A-Za-zÅÄÖåäö]+", text)}
    return bool(re.search(r"[åäöÅÄÖ]", text) or ord_ & SVENSKA_ORD)


def _rensa(text: str) -> str:
    text = re.sub(r"%[-+#0-9.*]*[A-Za-z%]", " ", text)
    text = re.sub(r"\{[^{}]*\}", " ", text)
    return " ".join(text.split())


def _go_bitar(fil: Path) -> list[Textbit]:
    innehall = fil.read_text(encoding="utf-8")
    bitar: list[Textbit] = []
    i = 0
    rad = 1
    while i < len(innehall):
        tecken = innehall[i]
        if tecken == "\n":
            rad += 1
            i += 1
            continue
        if innehall.startswith("//", i):
            slut = innehall.find("\n", i)
            slut = len(innehall) if slut < 0 else slut
            text = _rensa(innehall[i + 2:slut])
            if _ar_svenska(text):
                bitar.append(Textbit(fil, rad, "kommentar", text))
            i = slut
            continue
        if innehall.startswith("/*", i):
            slut = innehall.find("*/", i + 2)
            slut = len(innehall) - 2 if slut < 0 else slut
            text = _rensa(innehall[i + 2:slut])
            if _ar_svenska(text):
                bitar.append(Textbit(fil, rad, "kommentar", text))
            stycke = innehall[i:slut + 2]
            rad += stycke.count("\n")
            i = slut + 2
            continue
        if tecken == "`":
            slut = innehall.find("`", i + 1)
            slut = len(innehall) - 1 if slut < 0 else slut
            text = _rensa(innehall[i + 1:slut])
            if _ar_svenska(text):
                bitar.append(Textbit(fil, rad, "sträng", text))
            stycke = innehall[i:slut + 1]
            rad += stycke.count("\n")
            i = slut + 1
            continue
        if tecken == '"':
            start = i
            i += 1
            while i < len(innehall):
                if innehall[i] == "\\":
                    i += 2
                    continue
                if innehall[i] == '"':
                    i += 1
                    break
                i += 1
            literal = innehall[start:i]
            try:
                text = _rensa(ast.literal_eval(literal))
            except (SyntaxError, ValueError):
                text = ""
            if _ar_svenska(text):
                bitar.append(Textbit(fil, rad, "sträng", text))
            rad += literal.count("\n")
            continue
        if tecken == "'":
            i += 1
            while i < len(innehall):
                if innehall[i] == "\\":
                    i += 2
                    continue
                if innehall[i] == "'":
                    i += 1
                    break
                i += 1
            continue
        i += 1
    return bitar


class _HTMLTexter(HTMLParser):
    def __init__(self, fil: Path) -> None:
        super().__init__(convert_charrefs=True)
        self.fil = fil
        self.bitar: list[Textbit] = []
        self.ignorera = 0

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag in {"script", "style"}:
            self.ignorera += 1
        for namn, varde in attrs:
            text = _rensa(varde or "")
            if namn in ATTRIBUT and _ar_svenska(text):
                self.bitar.append(Textbit(self.fil, self.getpos()[0], f"attribut {namn}", text))

    def handle_endtag(self, tag: str) -> None:
        if tag in {"script", "style"} and self.ignorera:
            self.ignorera -= 1

    def handle_data(self, data: str) -> None:
        text = _rensa(data)
        if not self.ignorera and _ar_svenska(text):
            self.bitar.append(Textbit(self.fil, self.getpos()[0], "UI-text", text))


def _html_bitar(fil: Path) -> list[Textbit]:
    parser = _HTMLTexter(fil)
    parser.feed(fil.read_text(encoding="utf-8"))
    return parser.bitar


def _dokument_bitar(fil: Path) -> list[Textbit]:
    bitar: list[Textbit] = []
    kodblock = False
    for radnummer, radtext in enumerate(fil.read_text(encoding="utf-8").splitlines(), 1):
        if radtext.lstrip().startswith("```"):
            kodblock = not kodblock
            continue
        text = _rensa(re.sub(r"^\s*(?:#{1,6}|[-*+] |\d+[.)] )\s*", "", radtext))
        if not kodblock and _ar_svenska(text):
            bitar.append(Textbit(fil, radnummer, "dokumentation", text))
    return bitar


def _hamta_bitar() -> list[Textbit]:
    bitar: list[Textbit] = []
    for katalog in (ROT / "internal" / "pm", ROT / "internal" / "pmweb"):
        for fil in sorted(katalog.rglob("*.go")):
            # Testfilernas texter läser ingen användare, och de dränker fynden
            # i koden som faktiskt visas.
            if fil.name.endswith("_test.go"):
                continue
            bitar.extend(_go_bitar(fil))
    bitar.extend(_html_bitar(ROT / "internal" / "pmweb" / "static" / "pm.html"))
    dokument = [ROT / "README.md", *sorted((ROT / "docs").glob("*.md"))]
    for fil in dokument:
        if fil.is_file():
            bitar.extend(_dokument_bitar(fil))
    return bitar


def _ladda_linter(sokvag: Path) -> tuple[object, object] | None:
    specifikation = importlib.util.spec_from_file_location("klarsprak_lint", sokvag)
    if specifikation is None or specifikation.loader is None:
        print(f"Kunde inte läsa klarspråkslintern: {sokvag}")
        return None
    try:
        modul = importlib.util.module_from_spec(specifikation)
        specifikation.loader.exec_module(modul)
        return modul, modul.ladda_data()
    except Exception as fel:
        print(f"Kunde inte starta klarspråkslintern: {fel}")
        return None


def main() -> int:
    linter = Path(os.environ.get("KLARSPRAK_LINTER", STANDARD_LINTER)).expanduser()
    if not linter.is_file():
        print(f"Klarspråkslintern saknas: {linter}")
        print("Hoppar över rådgivande lintning. Sätt KLARSPRAK_LINTER till rätt sökväg.")
        return 0

    laddad_linter = _ladda_linter(linter)
    if laddad_linter is None:
        print("Hoppar över rådgivande lintning.")
        return 0
    modul, data = laddad_linter

    fynd = 0
    antal_per_kategori = {namn: 0 for namn in (*KATEGORIER, "anglicism", "em_dash")}
    for bit in _hamta_bitar():
        resultat = modul.lint(bit.text, data)
        overtradelser = resultat.get("overtradelser", {})
        kategorier = [namn for namn in KATEGORIER if overtradelser.get(namn, 0)]
        if resultat.get("anglicism", 0):
            kategorier.append("anglicism")
        if resultat.get("em_dash", 0):
            kategorier.append("em_dash")
        if not kategorier:
            continue
        for namn in kategorier:
            antal_per_kategori[namn] += int(
                overtradelser.get(namn, resultat.get(namn, 0))
            )
        fynd += 1
        sokvag = bit.fil.relative_to(ROT)
        utdrag = bit.text if len(bit.text) <= 100 else bit.text[:97] + "..."
        print(f"{sokvag}:{bit.rad}: {bit.slag}: {', '.join(kategorier)}")
        print(f"  {utdrag}")

    summering = ", ".join(
        f"{namn}={antal}" for namn, antal in antal_per_kategori.items() if antal
    )
    if summering:
        print(f"Kategoriantal: {summering}")
    print(f"Klarspråkslint: {fynd} textbitar med fynd. Resultatet är rådgivande.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
