# backlog-pm

backlog-pm är ett PM-lager ovanpå backlog. Du lägger in projekt och tasks,
delar ut arbete till agenter och följer deras körningar i webben. Varje projekt
kan också ha en testserver som PM startar och stoppar åt dig.

PM håller sina data i en egen profil. PM öppnar aldrig vardagsdatabasen i
backlog.

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

Två kommandon räcker.

```sh
backlog-pm init
backlog-pm web
```

`init` skapar PM:s workspace, registrerar profilen `pm` och skriver en
kommenterad `pm.toml`. Kommandot skriver sedan ut nästa steg.

PM frågar efter namn och lösenord. En färsk installation använder `admin` för
båda värdena. Byt lösenordet i `pm.toml` innan du öppnar PM mot nätverket.
Du kan också sätta miljövariabeln `BACKLOG_PM_LOSENORD`.

`web` lyssnar bara på den egna datorn. Använd `--bind 0.0.0.0` för åtkomst
från en annan maskin.

`web` börjar med port 6060. PM väljer nästa lediga port om porten är upptagen.
Kommandot skriver adressen som du öppnar i webbläsaren.

Välj **Nytt projekt** i vänsterspalten. PM kan skapa en ny arbetsmapp, koppla
ett befintligt repo eller klona ett tillåtet GitHub-repo.

Fyll i följande uppgifter:

- I **Aliasfältet** skriver du kortnamnet för kommandon, till exempel `webbshop`.
- **Namn** är namnet som PM visar i listorna.
- **Sökväg** är mappen under `~/workspace`.
- **Startkommando** är valfritt och startar projektets testserver.

Skriv `{port}` i startkommandot där porten ska stå. Citattecken fungerar inte
i det här fältet.

PM skapar ett testserverkort när projektet har ett startkommando. Där kan du
starta servern, stoppa den, öppna länken och läsa loggen.

## Vyerna i PM

Vänsterspalten innehåller sju vyer för det dagliga arbetet. **Nytt projekt**
öppnar formuläret som avsnittet ovan beskriver.

### Översikt

**Översikt** samlar alla aktiva projekt. Du kan dela ut en task genom att välja
projekt, taskreferens och valfri agent.

Vyn skiljer på pågående arbete och köade körningar. En köad rad förklarar också
varför arbetet väntar. **Väntar på mig** samlar fel, obesvarade agentinlägg och
viktiga tasks utan körning.

Här ser du även agenternas senaste kvotläge och alla projekts testservrar.
Kvotraden visar förbrukad andel i femtimmars- och sjudagarsfönster. Den visar
också nästa nollställning.

### Projekt

**Projekt** visar ett projekts tasks, pågående körningar, vänteläge och
blockeringar. Du kan även arkivera, återställa eller permanent ta bort
projektet.

Du kan skapa en task på två sätt. **Auto** låter en agent föreslå titel och
beskrivning från fri text. **Avancerat** låter dig ange fälten själv.

Tasklistans flikar visar `todo`, `doing`, `done` eller alla statusar. Du kan
söka i titel och referens. Du kan också filtrera på typ och prioritet.

Sortera efter prioritet, referens eller titel. Knappen **Kolumner** visar tre
statuskolumner sida vid sida. PM sparar dina listval i webbläsaren.

Varje task har verktyg för kommentarer, redigering, klassning och berikning.
Agentens klassning eller berikning blir ett redigerbart utkast. PM sparar bara
värden som du granskar och godkänner.

Knappen **Dela ut** öppnar agentvalet. Du kan välja agent, modell och
ansträngning. Ett tomt agentval låter reglerna välja.

Projektets testserver finns överst. Där startar och stoppar du servern, öppnar
dess adress och följer loggen.

### Samtal

**Samtal** är projektets bestående tråd. Ett vanligt inlägg stannar i tråden
utan agentanrop. Markera **Fråga agenten** om du vill ha ett svar.

Agenten får öppna tasks, projektminnet och de senaste inläggen som kontext.
Vyn visar agentens steg, tid och tokenmängd medan arbetet pågår. Du kan öppna
stegen igen efter en omladdning.

Filhändelser länkar till filvyn när filen finns i projektets repo. Agentsvar
visar rubriker, listor och kodblock från Markdown.

Agenten kan föreslå en post till projektminnet. Du kan redigera förslaget innan
du sparar det. Du kan också kvittera ett agentsvar utan ett nytt agentanrop.

Webbläsare med svenskt talstöd visar knapparna **Läs upp** och **Stoppa** vid
agentsvar.

### Kunskap

**Kunskap** samlar projektets docs och projektminne. Öppna en doc för att läsa
dess aktuella version, innehåll och författare.

Minneslistan visar varje posts text, taggar, aktör och tid. Vyn är till för
läsning. Använd backlog-kommandona eller samtalets minnesförslag för att skriva
ny kunskap.

### Filer

**Filer** låter dig bläddra i projektets repo utan att ändra filer. Vyn visar
inte `.git` och öppnar inte symboliska länkar.

Textvyn visar radnummer och syntaxfärger för vanliga filtyper. Du kan söka i
filen, växla radbrytning, kopiera innehållet eller sökvägen och ladda ner filen.

Vyn kan förhandsvisa bilder och video. Textfiler får vara högst 1 MiB. Bilder
och video får vara högst 100 MiB.

Sökfältet överst söker i repots textfiler. Resultatet visar fil, radnummer och
radens text. Sökningen stannar efter 100 träffar eller två sekunder.

Panelen **Ändrat i arbetsträdet** visar Git-status. Välj en ändrad fil för att
se dess diff mot `HEAD`.

### Körningar

**Körningar** visar pågående arbete och avslutad historik. Du kan filtrera
historiken på fel eller klara körningar och hämta fler äldre poster.

Knappen **Förlopp** visar agentens händelser medan körningen arbetar. Där ser du
bland annat verktyg, kommandon, filer och fel. Knappen **Logg** visar körningens
samlade utdata.

Körningsraden visar agent, motivering, status och eventuell exitkod. Taskens
referens öppnar den vanliga backlog-vyn. Samtalsfrågor länkar i stället till
projektsamtalet.

### Konfig

**Konfig** redigerar agenter, urvalsregler, testservrar, klarspråkskontroll och
en valfri anspråkskrok. Vyn skriver hela `pm.toml` när du sparar.

Kommentarer och okända nycklar försvinner då. Även fält som vyn inte visar kan
försvinna. Gör därför manuella ändringar efter din sista sparning i vyn.

## Agentkonfiguration

En agent består av ett kommando, argument och regler för indata och svar. Du
kan även ange timeout, miljö, modell, ansträngning, strömformat och MCP-stöd.

Välj en tom mall, Claude Code eller Codex när du lägger till en agent. Den
förvalda agenten tar arbetet när ingen regel väljer någon annan.

Regler matchar taskens typ, etiketter och nyckelord. PM använder den första
matchande regeln. Regeln kan också välja modell och ansträngning.

Knappen **Prova** kör den sparade agenten i ett tillfälligt Git-repo. Provet
kontrollerar kommandot och agentsvaret. Med MCP kontrollerar provet även
taskläsning, kommentar och rätt `ai:<agent>`-aktör.

Spara en ny eller ändrad agent före provet. Provknappen läser den sparade
konfigurationen.

Under **Beskriv ett agentverktyg** kan du förklara hur ett verktyg fungerar.
Den förvalda agenten föreslår då ett agentblock. PM märker blocket som ett
ogodkänt utkast.

Granska alltid kommando, argument, indata och svarsläge. Förslaget ändrar inte
`pm.toml` förrän du sparar hela konfigurationen.

## Kvotläge per agent

Översikten visar PM:s senaste kända kvotläge för varje agent. Claude kan lämna
kvoten i sin arbetsström. PM frågar Codex separat eftersom dess arbetsström
saknar kvotläget.

Andra agenttyper kan sakna kvotstöd. Då säger översikten att agenten inte
rapporterar något kvotläge.

PM kan byta agent när en kvot passerar ett valt tak. Du aktiverar bytet manuellt
i `pm.toml`:

```toml
[kvot]
tak = 0.9
turordning = ["claude", "codex"]
```

`tak` anger förbrukad andel. PM använder 0,9 när värdet saknas eller ligger
utanför intervallet. Bytet kräver färska uppgifter och en agent under taket.

Turordningen ska bara innehålla agenter som kan göra samma arbete. Ett eget
agentval gäller alltid, även när agentens kvot är full.

## GitHub-lagret

PM:s GitHub-lager rör bara repon i `tillatna_repon`. En tom lista nekar alla
repon. `sparrade_repon` väger alltid tyngre än tillåtelselistan.

Lägg inställningarna direkt i `pm.toml`:

```toml
[github]
tillatna_repon = ["agare/repo"]
sparrade_repon = ["agare/produktion"]
skrivlage = false
```

Lägg känsliga repon i spärrlistan. Då nekar PM dem även om de råkar finnas i
båda listorna.

Valet **Klona från GitHub** under **Nytt projekt** använder GitHub CLI. PM
kontrollerar tillåtelselistan före kloningen och verifierar repots `origin`
efteråt.

GitHub CLI behöver `GH_TOKEN` för anrop som kräver inloggning. PM använder en
isolerad `gh`-miljö och maskerar token i felmeddelanden.

Kontrollera installationen och behörigheten med:

```sh
backlog-pm github status
```

## Om något går fel

PM skriver fel på svenska och säger vad du ska göra. Några vanliga fel är:

- *Projektet har inget startkommando.* Lägg till ett testserverblock under
  **Konfig** eller fyll i startkommandot när du lägger till projektet.
- *Port 6060 är upptagen.* Kör `backlog-pm web` utan `--port`.
- *Profilen pm finns redan.* Du har redan kört `init`. Starta webben direkt.
- *GitHub-vakten nekade anropet.* Kontrollera tillåtelselistan, spärrlistan och
  `GH_TOKEN` med `backlog-pm github status`.

Testserverns utskrifter finns i loggrutan på projektsidan. Du kan också köra:

```sh
backlog-pm testserver logg <alias>
```

## Klarspråkslint

Kör `make lint-klarsprak` för att granska projektets svenska texter. Skriptet
granskar UI-text i `pm.html`, Go-strängar, Go-kommentarer och dokumentation.
Varje fynd visar ursprungsfilens namn och radnummer.

Skriptet läser linterns sökväg från `KLARSPRAK_LINTER`. Standardvärdet är
`~/workspace/klarspråk/klarsprak_lint.py`.

Skriptet hoppar över granskningen om filen saknas. Det skriver då ett tydligt
besked.

Lintningen är rådgivande. Skriptet rapporterar fynd men returnerar exitkod
noll. Exitkoden kan därför inte bevisa att texten saknar fynd.

Den första körningen den 12 september 2026 gav 132 textbitar med fynd. De flesta
fynd gällde linterns passivheuristik.

Granska långa agentprompter i Go-koden först. De innehåller ofta flera slags
fynd.

## När databasen blir beständig

PM-databasen slutar vara kastbar när projektet lämnar PM till en utomstående.
Gränsen kommer tidigare om någon måste bevara sparat arbete.

Från den tidpunkten får ingen ändra, flytta eller ta bort en släppt
migreringsfil. Varje schemaändring får en ny migreringsfil med nästa nummer.

PM kontrollerar databasens två schemaversioner när programmet öppnar databasen.
PM kör sedan saknade upstream- och PM-migreringar i nummerordning.

PM vägrar öppna databasen om någon schemaversion är för ny. Felmeddelandet
hänvisar till en nyare PM-version eller en kompatibel backup.

### Säkerhetskopiera och återställ

Stoppa PM och alla agentkörningar som skriver i databasen före en uppgradering.

Backupkommandot migrerar databasen innan det kopierar den. Använd därför den
gamla binären före installationen.

Skapa målkatalogen. Ta sedan en namngiven säkerhetskopia utanför PM:s workspace.

```sh
mkdir -p ~/backuper
cp "$(command -v backlog-pm)" ~/backuper/backlog-pm-gammal
backlog-pm doctor backup --profile pm --to ~/backuper/pm-före-uppgradering.db
```

Kontrollera att den gamla binären kan öppna säkerhetskopian:

```sh
~/backuper/backlog-pm-gammal doctor check --db ~/backuper/pm-före-uppgradering.db
```

Kommandot ska skriva att databasens integritet är ok. Behåll säkerhetskopian
tills du har kontrollerat uppgraderingen.

Återställ alltid till ett tomt workspace. Då lämnar du den nuvarande databasen
orörd.

```sh
mkdir -p ~/.config/backlog/pm-aterstallt
cp ~/backuper/pm-före-uppgradering.db ~/.config/backlog/pm-aterstallt/backlog.db
~/backuper/backlog-pm-gammal profile add pm-aterstallt --path ~/.config/backlog/pm-aterstallt
~/backuper/backlog-pm-gammal doctor check --profile pm-aterstallt
~/backuper/backlog-pm-gammal web --profile pm-aterstallt
```

Kontrollera projekt, tasks, samtal och körningar i det återställda workspacet.
PM stöder ingen automatisk nedgradering.
