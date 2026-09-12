# backlog-pm

backlog-pm är ett PM-lager ovanpå backlog. Du lägger in projekt och uppgifter,
delar ut arbetet till en agent och följer körningarna i en webbvy. Varje projekt
kan också ha en testserver som PM startar och stoppar åt dig.

PM håller sina data i en egen profil. Vardagsdatabasen i backlog rör den aldrig.

## Installera

Du bygger PM från källkoden. Du behöver Git och Go 1.25 eller senare. Stegen
fungerar likadant på Linux och macOS.

```sh
git clone https://github.com/Armandur/backlog.git
cd backlog
git checkout pm
make build-pm
mkdir -p ~/.local/bin
mv backlog-pm ~/.local/bin/
```

Ligger inte `~/.local/bin` i din PATH lägger du till den i ditt skal.

## Kom igång

Tre kommandon räcker.

```sh
backlog-pm init
backlog-pm web
```

`init` skapar PM:s workspace, registrerar profilen `pm` och skriver en
kommenterad `pm.toml`. Sedan skriver det ut vad du gör härnäst.

PM frågar efter namn och lösenord. Färsk installation har `admin` och `admin`,
som står i `pm.toml`. Byt lösenordet där innan du öppnar PM mot nätverket, eller
sätt miljövariabeln `BACKLOG_PM_LOSENORD`.

`web` lyssnar bara på den egna datorn. Vill du nå PM från en annan maskin startar
du med `--bind 0.0.0.0`.

`web` startar webben på port 6060. Är porten upptagen tar PM nästa lediga port
och skriver ut adressen. Öppna adressen i webbläsaren.

Välj **Nytt projekt** i vänsterspalten och fyll i:

- **Alias**: kortnamnet du använder i kommandon, till exempel `webbshop`.
- **Namn**: det som syns i listorna.
- **Sökväg**: mappen under `~/workspace`. PM kan skapa den, eller koppla ett
  Git-repo som redan finns.
- **Startkommando**: valfritt. Skriv kommandot som startar projektet lokalt,
  till exempel `npm run dev -- --port {port}`. PM sätter in porten där du
  skriver `{port}`.

Fyllde du i ett startkommando får projektet ett testserverkort. Knappen
**Starta** kör kommandot, och länken bredvid öppnar servern.

## Om något går fel

PM skriver fel på svenska och säger vad du ska göra. Några vanliga:

- *Projektet har inget startkommando.* Lägg till ett testserverblock under
  **Konfig**, eller fyll i startkommandot när du lägger till projektet.
- *Port 6060 är upptagen.* Kör `backlog-pm web` utan `--port` så letar PM själv.
- *Profilen pm finns redan.* Du har redan kört `init`. Starta webben direkt.

Testserverns utskrifter läser du i loggrutan på projektsidan, eller med
`backlog-pm testserver logg <alias>`.

## Klarspråkslint

Kör `make lint-klarsprak` för att granska projektets svenska texter.
Skriptet granskar UI-text i `pm.html`, Go-strängar, Go-kommentarer och dokumentation.
Varje fynd visar ursprungsfilens namn och radnummer.

Skriptet läser linterns sökväg från `KLARSPRAK_LINTER`.
Standardvärdet är `~/workspace/klarspråk/klarsprak_lint.py`.
Om filen saknas hoppar skriptet över granskningen och skriver ett tydligt besked.

Lintningen är rådgivande.
Skriptet rapporterar fynd men returnerar exitkod noll.
Den blockerar därför inte annat arbete.

Första körningen den 12 september 2026 gav 132 textbitar med fynd.
De flesta fynd gäller linterns passivheuristik.
Granska långa agentprompter i Go-koden först eftersom de har mer sammansatta fynd.

## När databasen blir beständig

PM-databasen slutar vara kastbar när projektet lämnar PM till en utomstående. Gränsen kommer tidigare om någon måste bevara sparat arbete.

Från den tidpunkten får ingen ändra, flytta eller ta bort en släppt migreringsfil. Varje schemaändring får en ny migreringsfil med nästa nummer.

Varje start ska först kontrollera databasens två schemaversioner. PM ska sedan köra saknade upstream- och PM-migreringar i ordning.

PM ska vägra starta om någon schemaversion är nyare än programmets version. Felmeddelandet ska hänvisa till en kompatibel programversion eller backup.

Ta en backup med den gamla programversionen före en uppgradering:

```sh
backlog-pm doctor backup --to /sökväg/pm-före-uppgradering.db
```

Behåll backupen tills du har kontrollerat uppgraderingen. PM stöder ingen nedgradering av databasen.

Dagens kod uppfyller inte hela avtalet. Vanliga kommandon migrerar databasen före användning, men `init` kör bara upstreams migreringar.
PM godtar också en databas med en okänd, nyare schemaversion. Räkna därför databasen som kastbar tills uppföljningstaskerna i utredningen är klara.
