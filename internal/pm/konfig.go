package pm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// KonfigFil är namnet på PM-konfigurationen i profilens workspace-katalog.
const KonfigFil = "pm.toml"

// AgentKonfig beskriver hur en agent startas. Allt som behövs för en ny agent
// står här, så en tredje agent läggs till i konfigfilen utan kodändring.
type AgentKonfig struct {
	Kommando string   `toml:"kommando"`
	Args     []string `toml:"args"`
	// Brief: "arg" (redan i args via {brief}) eller "stdin".
	Brief string `toml:"brief"`
	// Svar: "stdout" eller "fil" (läses från {svarsfil}).
	Svar string `toml:"svar"`
	// Stdin: "devnull" stänger stdin, vilket codex kräver.
	Stdin           string            `toml:"stdin"`
	TimeoutSekunder int               `toml:"timeout_sekunder"`
	Miljo           map[string]string `toml:"miljo"`
	// MCP lägger till --mcp-config mot PM-profilen (claude headless).
	MCP bool `toml:"mcp"`
}

// Regel väljer agent utifrån taskens typ, etiketter och nyckelord.
type Regel struct {
	Namn      string   `toml:"namn"`
	Typ       []string `toml:"typ"`
	Etiketter []string `toml:"etiketter"`
	Nyckelord []string `toml:"nyckelord"`
	Agent     string   `toml:"agent"`
}

// Krok kan anropa ett externt anspråkskommando, t.ex. arbetar. Kön fungerar
// utan krok.
type Krok struct {
	Anspraka []string `toml:"anspraka"`
	Slapp    []string `toml:"slapp"`
}

// Konfig är hela PM-konfigurationen.
type Konfig struct {
	DefaultAgent string                 `toml:"default_agent"`
	Agenter      map[string]AgentKonfig `toml:"agenter"`
	Regler       []Regel                `toml:"regler"`
	Krok         Krok                   `toml:"krok"`
	// Kalla är sökvägen filen lästes från, tom när defaulterna används.
	Kalla string `toml:"-"`
}

// StandardKonfig är det som gäller när ingen pm.toml finns.
func StandardKonfig() Konfig {
	return Konfig{
		DefaultAgent: "claude",
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
	if err := k.Validera(); err != nil {
		return Konfig{}, err
	}
	return k, nil
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

// Validera fångar konfigfel innan en körning startas.
func (k Konfig) Validera() error {
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
	return nil
}

func harPlatshallare(args []string, namn string) bool {
	for _, a := range args {
		if strings.Contains(a, namn) {
			return true
		}
	}
	return false
}
