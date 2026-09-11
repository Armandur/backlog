package pm

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	Brief       string
	Repo        string
	Logg        string
	Svarsfil    string
	Profil      string
	TaskRef     string
	PMBinar     string
	VidHandelse func(Handelse)
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

	args := make([]string, 0, len(a.konfig.Args)+3)
	for _, arg := range a.konfig.Args {
		args = append(args, ersattPlatshallare(arg, in))
	}
	if a.konfig.Strom == "claude-json" {
		args = append(args, "--output-format", "stream-json", "--verbose")
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

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Resultat{}, fmt.Errorf("kunde inte läsa standardutdata från agenten %s: %w", a.namn, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Resultat{}, fmt.Errorf("kunde inte läsa felutdata från agenten %s: %w", a.namn, err)
	}
	if err := cmd.Start(); err != nil {
		return Resultat{}, fmt.Errorf("kunde inte köra agenten %s (%s): %w", a.namn, a.konfig.Kommando, err)
	}

	var utdata synkadUtdata
	var lasare sync.WaitGroup
	lasfel := make(chan error, 2)
	lasare.Add(2)
	go func() {
		defer lasare.Done()
		forLang := func(tecken int) {
			text := fmt.Sprintf("agenten skrev en rad på %d tecken, som PM hoppade över", tecken)
			utdata.skrivRad([]byte("[PM] " + text + "\n"))
			if in.VidHandelse != nil {
				in.VidHandelse(nyHandelse("fel", text))
			}
		}
		if err := lasRader(stdout, func(rad []byte) {
			utdata.skrivRad(rad)
			if a.konfig.Strom == "claude-json" && in.VidHandelse != nil {
				for _, handelse := range tolkaClaudeRad(rad) {
					in.VidHandelse(handelse)
				}
			}
		}, forLang); err != nil {
			lasfel <- err
		}
	}()
	go func() {
		defer lasare.Done()
		forLangStderr := func(tecken int) {
			utdata.skrivRad([]byte(fmt.Sprintf("[PM] agenten skrev en felrad på %d tecken, som PM hoppade över\n", tecken)))
		}
		if err := lasRader(stderr, utdata.skrivRad, forLangStderr); err != nil {
			lasfel <- err
		}
	}()
	lasare.Wait()
	korfel := cmd.Wait()
	close(lasfel)
	for err := range lasfel {
		if korfel == nil {
			korfel = fmt.Errorf("kunde inte läsa agentens utdata: %w", err)
		}
	}

	samladUtdata := utdata.String()
	res := Resultat{ExitKod: cmd.ProcessState.ExitCode(), Logg: in.Logg}
	if res.ExitKod < 0 {
		res.ExitKod = 1
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitKod = 124
	}

	res.Utdata = samladUtdata
	if a.konfig.Strom == "claude-json" {
		// Strömmen är maskinläsbar. Kommentaren på tasken ska bära svaret.
		if svar := SvarUrClaudeStrom(samladUtdata); svar != "" {
			res.Utdata = svar
		}
	}
	if a.konfig.Svar == "fil" {
		if data, err := os.ReadFile(in.Svarsfil); err == nil && strings.TrimSpace(string(data)) != "" {
			res.Utdata = string(data)
		}
	}
	if in.Logg != "" {
		skrivLogg(in.Logg, a.namn, in.Brief, samladUtdata, res.ExitKod)
	}
	if korfel != nil {
		if _, ok := korfel.(*exec.ExitError); !ok {
			return res, fmt.Errorf("kunde inte slutföra agenten %s (%s): %w", a.namn, a.konfig.Kommando, korfel)
		}
	}
	return res, nil
}

const storstaUtdataRad = 16 * 1024 * 1024

type synkadUtdata struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (u *synkadUtdata) skrivRad(rad []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.b.Write(rad)
}

func (u *synkadUtdata) String() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.b.String()
}

// lasRader läser utdata rad för rad. En rad som är större än taket kastas i
// bitar i stället för att stoppa läsningen. Slutar vi läsa fylls processens
// pipe, och agenten hänger sedan tills timeouten slår till.
//
// Raden som hantera får återanvänds nästa varv. Den som sparar den måste kopiera.
func lasRader(r io.Reader, hantera func([]byte), forLang func(int)) error {
	lasare := bufio.NewReaderSize(r, 64*1024)
	var rad []byte
	kastat := 0
	for {
		del, err := lasare.ReadSlice('\n')
		if len(rad)+kastat+len(del) > storstaUtdataRad {
			kastat += len(del)
		} else {
			rad = append(rad, del...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		klar := len(rad) > 0 || kastat > 0
		switch {
		case kastat > 0:
			if forLang != nil {
				forLang(len(rad) + kastat)
			}
		case klar:
			hantera(rad)
		}
		rad, kastat = rad[:0], 0
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
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
