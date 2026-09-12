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
