package pm

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	_ "modernc.org/sqlite"
)

// KonfigFil är namnet på PM-konfigurationen i profilens workspace-katalog.
const KonfigFil = "pm.toml"

// AgentKonfig beskriver hur en agent startas. Allt som behövs för en ny agent
// står här, så en tredje agent läggs till i konfigfilen utan kodändring.
type AgentKonfig struct {
	Kommando string   `toml:"kommando" json:"kommando"`
	Args     []string `toml:"args" json:"args"`
	// Brief: "arg" (redan i args via {brief}) eller "stdin".
	Brief string `toml:"brief" json:"brief"`
	// Svar: "stdout" eller "fil" (läses från {svarsfil}).
	Svar string `toml:"svar" json:"svar"`
	// Strom: "claude-json", "codex-json" eller tomt för vanlig utdata.
	Strom string `toml:"strom" json:"strom"`
	// Stdin: "devnull" stänger stdin, vilket codex kräver.
	Stdin           string            `toml:"stdin" json:"stdin"`
	TimeoutSekunder int               `toml:"timeout_sekunder" json:"timeout_sekunder"`
	Miljo           map[string]string `toml:"miljo" json:"miljo"`
	// MCP lägger till --mcp-config mot PM-profilen (claude headless).
	MCP bool `toml:"mcp" json:"mcp"`
	// Modell och Anstrangning fyller {modell} och {anstrangning} i args när
	// varken utdelningen eller regeln säger något annat.
	Modell       string `toml:"modell" json:"modell"`
	Anstrangning string `toml:"anstrangning" json:"anstrangning"`
}

// Regel väljer agent utifrån taskens typ, etiketter och nyckelord.
type Regel struct {
	Namn      string   `toml:"namn" json:"namn"`
	Typ       []string `toml:"typ" json:"typ"`
	Etiketter []string `toml:"etiketter" json:"etiketter"`
	Nyckelord []string `toml:"nyckelord" json:"nyckelord"`
	Agent     string   `toml:"agent" json:"agent"`
	// Regeln får sätta modell och ansträngning för de tasks den fångar.
	Modell       string `toml:"modell" json:"modell"`
	Anstrangning string `toml:"anstrangning" json:"anstrangning"`
}

// Krok kan anropa ett externt anspråkskommando, t.ex. arbetar. Kön fungerar
// utan krok.
type Krok struct {
	Anspraka []string          `toml:"anspraka" json:"anspraka"`
	Slapp    []string          `toml:"slapp" json:"slapp"`
	Miljo    map[string]string `toml:"miljo" json:"miljo"`
}

// TestserverKonfig beskriver hur ett projekts testserver senare ska startas.
// Den här konfigurationen startar ingen process.
type TestserverKonfig struct {
	Kommando string            `toml:"kommando" json:"kommando"`
	Args     []string          `toml:"args" json:"args"`
	CWD      string            `toml:"cwd" json:"cwd"`
	Port     int               `toml:"port" json:"port"`
	Halsa    string            `toml:"halsa" json:"halsa"`
	Miljo    map[string]string `toml:"miljo" json:"miljo"`
}

// PortKonfig anger intervallet som PM använder för automatiska portar.
type PortKonfig struct {
	Fran int `toml:"fran" json:"fran"`
	Till int `toml:"till" json:"till"`
}

// WebbKonfig är inloggningen till PM-webben. Lösenordet kan också komma från
// miljövariabeln BACKLOG_PM_LOSENORD, som vinner över filen.
type WebbKonfig struct {
	Anvandare string `toml:"anvandare" json:"anvandare"`
	Losenord  string `toml:"losenord" json:"losenord"`
}

// Konfig är hela PM-konfigurationen.
type Konfig struct {
	DefaultAgent string                      `toml:"default_agent" json:"default_agent"`
	Agenter      map[string]AgentKonfig      `toml:"agenter" json:"agenter"`
	Regler       []Regel                     `toml:"regler" json:"regler"`
	Krok         Krok                        `toml:"krok" json:"krok"`
	Portar       PortKonfig                  `toml:"portar" json:"portar"`
	Testserver   map[string]TestserverKonfig `toml:"testserver" json:"testserver"`
	Webb         WebbKonfig                  `toml:"webb" json:"webb"`
	// Kalla är sökvägen konfigurationen kommer från, tom när PM använder defaulterna.
	Kalla string `toml:"-" json:"-"`
}

// StandardKonfig är det som gäller när ingen pm.toml finns.
func StandardKonfig() Konfig {
	return Konfig{
		DefaultAgent: "claude",
		Portar:       PortKonfig{Fran: 8100, Till: 8199},
		Agenter: map[string]AgentKonfig{
			"claude": {
				Kommando:        "claude",
				Args:            []string{"-p", "{brief}"},
				Brief:           "arg",
				Svar:            "stdout",
				Stdin:           "devnull",
				TimeoutSekunder: 900,
				MCP:             true,
			},
			"codex": {
				// Kontraktet från codex-delegat: stdin stängd, svaret i -o-filen.
				Kommando:        "codex",
				Args:            []string{"exec", "-C", "{repo}", "-s", "workspace-write", "-c", "sandbox_workspace_write.network_access=true", "-o", "{svarsfil}", "{brief}"},
				Brief:           "arg",
				Svar:            "fil",
				Stdin:           "devnull",
				TimeoutSekunder: 900,
			},
		},
	}
}

// LasKonfig läser pm.toml ur workspace-katalogen. Saknas filen används
// standardkonfigurationen.
func LasKonfig(workspaceDir string) (Konfig, error) {
	sokvag := filepath.Join(workspaceDir, KonfigFil)
	data, err := os.ReadFile(sokvag)
	if os.IsNotExist(err) {
		return StandardKonfig(), nil
	}
	if err != nil {
		return Konfig{}, fmt.Errorf("läs %s: %w", sokvag, err)
	}
	k := Konfig{}
	if err := toml.Unmarshal(data, &k); err != nil {
		return Konfig{}, fmt.Errorf("%s är trasig: %w", sokvag, err)
	}
	k.Kalla = sokvag
	std := StandardKonfig()
	if k.Portar.Fran == 0 && k.Portar.Till == 0 {
		k.Portar = std.Portar
	}
	if len(k.Agenter) == 0 {
		k.Agenter = std.Agenter
	}
	if k.DefaultAgent == "" {
		k.DefaultAgent = std.DefaultAgent
		if _, finns := k.Agenter[k.DefaultAgent]; !finns {
			for namn := range k.Agenter {
				k.DefaultAgent = namn
				break
			}
		}
	}
	for namn, a := range k.Agenter {
		k.Agenter[namn] = fyllIStandard(a)
	}
	// Läsningen kontrollerar inte att projekten finns. Ett kvarglömt block för
	// ett raderat projekt ska inte kunna spärra hela konfigvyn, för då går det
	// inte att ta bort blocket där heller.
	if err := k.Validera(); err != nil {
		return Konfig{}, err
	}
	return k, nil
}

// SkrivKonfig validerar och ersätter pm.toml atomiskt. Här kontrolleras även
// att varje testserver pekar på ett projekt som finns, för här går felet att
// rätta i samma anrop.
func SkrivKonfig(workspaceDir string, k Konfig) error {
	projektalias, err := lasProjektalias(workspaceDir)
	if err != nil {
		return err
	}
	if err := k.Validera(projektalias...); err != nil {
		return err
	}

	var data bytes.Buffer
	if err := toml.NewEncoder(&data).Encode(k); err != nil {
		return fmt.Errorf("kunde inte skapa konfigurationsfilen: %w", err)
	}

	sokvag := filepath.Join(workspaceDir, KonfigFil)
	tmp, err := os.CreateTemp(workspaceDir, ".pm.toml-*")
	if err != nil {
		return fmt.Errorf("kunde inte skapa en temporär konfigurationsfil: %w", err)
	}
	tmpNamn := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpNamn)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("kunde inte skydda den temporära konfigurationsfilen: %w", err)
	}
	if _, err := tmp.Write(data.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("kunde inte skriva den temporära konfigurationsfilen: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("kunde inte synka den temporära konfigurationsfilen: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("kunde inte stänga den temporära konfigurationsfilen: %w", err)
	}
	if err := os.Rename(tmpNamn, sokvag); err != nil {
		return fmt.Errorf("kunde inte ersätta %s: %w", sokvag, err)
	}
	renamed = true
	return nil
}

func fyllIStandard(a AgentKonfig) AgentKonfig {
	if a.Brief == "" {
		a.Brief = "arg"
	}
	if a.Svar == "" {
		a.Svar = "stdout"
	}
	if a.TimeoutSekunder <= 0 {
		a.TimeoutSekunder = 900
	}
	return a
}

// Validera fångar konfigfel innan PM startar en körning.
func (k Konfig) Validera(projektalias ...string) error {
	portar := k.Portar
	if portar.Fran == 0 && portar.Till == 0 {
		portar = PortKonfig{Fran: 8100, Till: 8199}
	}
	if err := (PortIntervall{Fran: portar.Fran, Till: portar.Till}).validera(); err != nil {
		return err
	}
	for namn, a := range k.Agenter {
		if a.Kommando == "" {
			return fmt.Errorf("agenten %q saknar kommando i konfigurationen", namn)
		}
		switch a.Brief {
		case "arg", "stdin":
		default:
			return fmt.Errorf("agenten %q har okänt brief-läge %q, använd arg eller stdin", namn, a.Brief)
		}
		switch a.Svar {
		case "stdout", "fil":
		default:
			return fmt.Errorf("agenten %q har okänt svar-läge %q, använd stdout eller fil", namn, a.Svar)
		}
		switch a.Strom {
		case "", "claude-json", "codex-json":
		default:
			return fmt.Errorf("agenten %q har okänt strömformat %q, använd claude-json eller codex-json", namn, a.Strom)
		}
		if a.Svar == "fil" && !harPlatshallare(a.Args, "{svarsfil}") {
			return fmt.Errorf("agenten %q läser svaret från fil men saknar {svarsfil} i args", namn)
		}
		if a.Brief == "arg" && !harPlatshallare(a.Args, "{brief}") {
			return fmt.Errorf("agenten %q skickar briefen som argument men saknar {brief} i args", namn)
		}
	}
	for i, r := range k.Regler {
		if r.Agent == "" {
			return fmt.Errorf("regel %d saknar agent", i+1)
		}
		if _, finns := k.Agenter[r.Agent]; !finns {
			return fmt.Errorf("regel %d pekar på agenten %q som inte finns i konfigurationen", i+1, r.Agent)
		}
	}
	if k.DefaultAgent != "" {
		if _, finns := k.Agenter[k.DefaultAgent]; !finns {
			return fmt.Errorf("default_agent %q finns inte bland agenterna", k.DefaultAgent)
		}
	}
	projekt := make(map[string]bool, len(projektalias))
	for _, alias := range projektalias {
		projekt[alias] = true
	}
	for alias, server := range k.Testserver {
		if projektalias != nil && !projekt[alias] {
			return fmt.Errorf("testservern pekar på projektet %q som inte finns", alias)
		}
		if strings.TrimSpace(server.Kommando) == "" {
			return fmt.Errorf("testservern för projektet %q saknar kommando", alias)
		}
		if server.Port == 0 && !harPlatshallare(server.Args, "{port}") {
			return fmt.Errorf("testservern för projektet %q saknar både fast port och {port} i args", alias)
		}
		if server.CWD != "" {
			info, err := os.Stat(server.CWD)
			if os.IsNotExist(err) {
				return fmt.Errorf("testservern för projektet %q har cwd %q, men katalogen saknas", alias, server.CWD)
			}
			if err != nil {
				return fmt.Errorf("kunde inte kontrollera cwd %q för projektet %q: %w", server.CWD, alias, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("testservern för projektet %q har cwd %q, men sökvägen är ingen katalog", alias, server.CWD)
			}
		}
	}
	return nil
}

func lasProjektalias(workspaceDir string) ([]string, error) {
	databas := filepath.Join(workspaceDir, "backlog.db")
	if _, err := os.Stat(databas); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("kunde inte kontrollera projektdatabasen: %w", err)
	}
	uri := (&url.URL{Scheme: "file", Path: databas, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("kunde inte öppna projektdatabasen: %w", err)
	}
	defer db.Close()
	rader, err := db.Query("SELECT alias FROM projects WHERE archived_at IS NULL")
	if err != nil {
		return nil, fmt.Errorf("kunde inte läsa projekten: %w", err)
	}
	defer rader.Close()
	alias := make([]string, 0)
	for rader.Next() {
		var namn string
		if err := rader.Scan(&namn); err != nil {
			return nil, fmt.Errorf("kunde inte läsa ett projektalias: %w", err)
		}
		alias = append(alias, namn)
	}
	if err := rader.Err(); err != nil {
		return nil, fmt.Errorf("kunde inte läsa projekten: %w", err)
	}
	return alias, nil
}

func harPlatshallare(args []string, namn string) bool {
	for _, a := range args {
		if strings.Contains(a, namn) {
			return true
		}
	}
	return false
}
