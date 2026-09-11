package pm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Resultat är utfallet av en agentkörning.
type Resultat struct {
	Utdata  string
	ExitKod int
	Logg    string
}

// Korare kör en brief och rapporterar exitkod. Utdelaren pratar bara med det
// här gränssnittet, så en fejkad körare kan ta agentens plats i test.
type Korare interface {
	Namn() string
	Kor(ctx context.Context, in KorInput) (Resultat, error)
}

// KorInput är det en körning behöver: briefen, repo-katalogen och var loggen
// och agentens svarsfil ska ligga.
type KorInput struct {
	Brief    string
	Repo     string
	Logg     string
	Svarsfil string
	Profil   string
	TaskRef  string
	PMBinar  string
}

// KommandoAgent är den enda agentimplementationen. Allt som skiljer claude
// från codex eller ett eget skalskript står i konfigurationen.
type KommandoAgent struct {
	namn   string
	konfig AgentKonfig
}

func NewKommandoAgent(namn string, k AgentKonfig) *KommandoAgent {
	return &KommandoAgent{namn: namn, konfig: fyllIStandard(k)}
}

func (a *KommandoAgent) Namn() string { return a.namn }

// Konfig ger agentens konfiguration, för besked och felsökning.
func (a *KommandoAgent) Konfig() AgentKonfig { return a.konfig }

// Fraga är samtalsvägen: samma kommando, svaret som text.
func (a *KommandoAgent) Fraga(ctx context.Context, prompt string) (string, error) {
	tmp, err := os.MkdirTemp("", "backlog-pm-fraga-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	res, err := a.Kor(ctx, KorInput{Brief: prompt, Svarsfil: tmp + "/svar.txt"})
	if err != nil {
		return "", err
	}
	if res.ExitKod != 0 {
		return "", fmt.Errorf("agenten %s avslutade med exitkod %d: %s", a.namn, res.ExitKod, kortText(res.Utdata))
	}
	if strings.TrimSpace(res.Utdata) == "" {
		return "", fmt.Errorf("agenten %s gav ett tomt svar", a.namn)
	}
	return strings.TrimSpace(res.Utdata), nil
}

// Kor startar agenten och läser svaret enligt konfigurationen.
func (a *KommandoAgent) Kor(ctx context.Context, in KorInput) (Resultat, error) {
	if in.Svarsfil == "" {
		f, err := os.CreateTemp("", "backlog-pm-svar-*.txt")
		if err != nil {
			return Resultat{}, err
		}
		f.Close()
		in.Svarsfil = f.Name()
		defer os.Remove(in.Svarsfil)
	}

	ctx, avbryt := context.WithTimeout(ctx, time.Duration(a.konfig.TimeoutSekunder)*time.Second)
	defer avbryt()

	args := make([]string, 0, len(a.konfig.Args)+2)
	for _, arg := range a.konfig.Args {
		args = append(args, ersattPlatshallare(arg, in))
	}
	if a.konfig.MCP {
		if cfg, stad, err := mcpConfigFil(in.PMBinar, in.Profil, "ai:"+a.namn); err == nil && cfg != "" {
			defer stad()
			args = append(args, "--mcp-config", cfg)
		}
	}

	cmd := exec.CommandContext(ctx, a.konfig.Kommando, args...)
	if in.Repo != "" {
		cmd.Dir = in.Repo
	}
	if a.konfig.Brief == "stdin" {
		cmd.Stdin = strings.NewReader(in.Brief)
	} else {
		// Codex läser pipad stdin till EOF och hänger annars.
		cmd.Stdin = nil
	}
	if len(a.konfig.Miljo) > 0 {
		cmd.Env = os.Environ()
		for k, v := range a.konfig.Miljo {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	utdata, korfel := cmd.CombinedOutput()
	res := Resultat{ExitKod: cmd.ProcessState.ExitCode(), Logg: in.Logg}
	if res.ExitKod < 0 {
		res.ExitKod = 1
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitKod = 124
	}

	res.Utdata = string(utdata)
	if a.konfig.Svar == "fil" {
		if data, err := os.ReadFile(in.Svarsfil); err == nil && strings.TrimSpace(string(data)) != "" {
			res.Utdata = string(data)
		}
	}
	if in.Logg != "" {
		skrivLogg(in.Logg, a.namn, in.Brief, string(utdata), res.ExitKod)
	}
	if korfel != nil {
		if _, ok := korfel.(*exec.ExitError); !ok {
			// Kommandot gick inte att starta alls.
			return res, fmt.Errorf("kunde inte köra agenten %s (%s): %w", a.namn, a.konfig.Kommando, korfel)
		}
	}
	return res, nil
}

func ersattPlatshallare(arg string, in KorInput) string {
	byten := strings.NewReplacer(
		"{brief}", in.Brief,
		"{repo}", in.Repo,
		"{svarsfil}", in.Svarsfil,
		"{logg}", in.Logg,
		"{task}", in.TaskRef,
		"{profil}", in.Profil,
	)
	return byten.Replace(arg)
}

func skrivLogg(sokvag, agent, brief, utdata string, exitkod int) {
	f, err := os.Create(sokvag)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "agent: %s\ntid: %s\n\n--- brief ---\n%s\n\n--- utdata ---\n%s\n\nexitkod: %d\n",
		agent, time.Now().Format(time.RFC3339), brief, utdata, exitkod)
}

// mcpConfigFil skriver MCP-konfigen agenten får. Aktören måste med, annars
// faller backlog tillbaka på $USER och skriver agentens arbete i Rasmus namn.
func mcpConfigFil(pmBinar, profil, aktor string) (string, func(), error) {
	if pmBinar == "" || profil == "" {
		return "", func() {}, nil
	}
	f, err := os.CreateTemp("", "backlog-pm-mcp-*.json")
	if err != nil {
		return "", func() {}, err
	}
	cfg := fmt.Sprintf(`{"mcpServers":{"backlog-pm":{"command":%q,"args":["--profile",%q,"--as",%q,"mcp","serve"]}}}`, pmBinar, profil, aktor)
	if _, err := f.WriteString(cfg); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

func kortText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
