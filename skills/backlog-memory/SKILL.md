---
name: backlog-memory
description: Läs in (learn) eller spara (store) projektkontext i backlog memory - den agent-agnostiska projektminnet som både Claude- och Codex-sessioner läser. Använd vid /backlog-memory <alias>, "läs in projektkontexten", "spara beslutet i backlog memory", "vad vet vi om projektet", vid sessionsstart i ett backlog-spårat projekt med tomt kontextläge, eller efter betydande arbete/beslut som nästa session (oavsett verktyg) behöver känna till.
---

# backlog-memory

Backlog memory är projektminnet i backlog-databasen: agent-agnostiskt (Claude
och Codex läser samma), attribuerat per skrivning och backat upp med databasen.
Det kompletterar Claudes egen auto-memory (`~/.claude/projects/*/memory/`) som
är Claude-privat.

## Delningsregeln - vad hör hemma var

- **Backlog memory:** projektfakta och beslut - arkitekturval, konventioner,
  nuläge, "vi valde X för att Y". Allt en annan agent/session behöver.
- **Agentens egen memory:** beteende-feedback till just den agenten ("Rasmus
  vill att jag ..."). Globala beteenderegler befordras till CLAUDE.md
  respektive AGENTS.md, inte hit.

## Learn (läs in kontext)

Vid sessionsstart i ett backlog-spårat projekt, eller på begäran:

```sh
backlog memory list --project <alias> --json --profile default
backlog doc list --project <alias> --json --profile default
```

Lyft in relevanta poster som kontext innan du svarar - det förhindrar att
redan fattade beslut härleds om. Tomt resultat är ett giltigt svar: hitta inte
på kontext, och föreslå store efter passets första riktiga arbete.

## Store (spara kontext)

Efter betydande arbete eller beslut:

```sh
backlog memory add "<en post = ett faktum/beslut>" \
  --project <alias> --tag "decision" \
  --as ai:<modell> --profile default
```

- En post per faktum, skrivet som nuläge (inga historienarrativ - historiken
  finns i attributeringen och git).
- Taggar: `decision` (vägval + varför), `status` (nuläge/öppna trådar),
  `convention` (projektkonvention).
- Skriv ALDRIG hemligheter eller credentials i memory.
- **Det finns inget `update`-kommando.** Ändra en post med
  `backlog memory append <id> <text>` (lägg till) eller
  `backlog memory delete <id>` + ny `add` (ersätt). `id` kommer från
  `backlog memory list`. Läs `list` först - komplettera/ersätt hellre än
  duplicera. En rullande `status`-post uppdateras genom delete + add, inte
  genom att lägga en till bredvid.

## Ambivalent anrop

`/backlog-memory <alias>` utan mer: learn vid sessionsstart, store efter
utfört arbete. Oklart vilket - fråga.
