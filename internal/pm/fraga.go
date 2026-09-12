package pm

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mazen160/backlog/internal/models"
)

// FragaInput är en fråga till en agent i ett projektsamtal.
type FragaInput struct {
	Alias        string
	ProjectID    string
	Fraga        string
	Agent        string
	Fragare      models.Actor
	Granser      KontextGranser
	WorkspaceDir string
	Profil       string
	PMBinar      string
}

// StartadFraga kopplar frågans inlägg till körningen som skriver svaret.
type StartadFraga struct {
	Inlagg  *Inlagg  `json:"inlagg"`
	Korning *Korning `json:"korning"`
}

// StartaFraga sparar frågan, skapar körningen och låter agenten svara i
// bakgrunden. Körningen saknar task eftersom svaret hör till samtalet.
func StartaFraga(ctx context.Context, db *sql.DB, reg *AgentRegister, in FragaInput) (*StartadFraga, error) {
	if reg == nil {
		return nil, fmt.Errorf("ingen agent är registrerad")
	}
	korare, err := reg.HamtaKorare(in.Agent)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.WorkspaceDir) == "" {
		// Utan workspace hamnar körningens loggar i arbetskatalogen, alltså i
		// repot när PM körs därifrån.
		return nil, fmt.Errorf("frågan saknar workspace, så PM vet inte var loggen ska ligga")
	}
	store := NewSamtalStore(db)
	fraga, err := store.Add(ctx, in.ProjectID, "", in.Fragare, in.Fraga)
	if err != nil {
		return nil, err
	}
	kontext, err := ByggKontext(ctx, db, in.Alias, in.ProjectID, in.Granser)
	if err != nil {
		return nil, err
	}
	var repoPath string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(repo_path,'') FROM projects WHERE id=?`, in.ProjectID).Scan(&repoPath); err != nil {
		return nil, fmt.Errorf("läs projektets repo: %w", err)
	}
	loggkatalog := LoggKatalog(in.WorkspaceDir)
	if err := os.MkdirAll(loggkatalog, 0o755); err != nil {
		return nil, fmt.Errorf("skapa körningens loggkatalog: %w", err)
	}
	korning := &Korning{
		Sort: KorningSortFraga, ProjectID: in.ProjectID, Agent: korare.Namn(),
		Motivering: "fråga i projektsamtalet", Status: StatusKoad, RepoPath: repoPath,
	}
	korningsstore := NewKorningStore(db)
	if err := korningsstore.Skapa(ctx, korning); err != nil {
		return nil, err
	}
	korning.Logg = filepath.Join(loggkatalog, korning.ID+".log")
	if err := korningsstore.SattLogg(ctx, korning.ID, korning.Logg); err != nil {
		return nil, err
	}
	if err := korningsstore.SattStatus(ctx, korning.ID, StatusKor); err != nil {
		return nil, err
	}
	korning.Status = StatusKor
	prompt := ByggPrompt(kontext, in.Fraga)
	go korFraga(context.WithoutCancel(ctx), db, korare, korning, prompt, in)
	return &StartadFraga{Inlagg: fraga, Korning: korning}, nil
}

// utanMinnesmarkor tar bort PM:s egen minnesmarkör ur händelserna. Markören
// är till för PM, och användaren ska inte se den i förloppet.
func utanMinnesmarkor(skriv func(Handelse)) func(Handelse) {
	return func(h Handelse) {
		text, _ := delaAgentsvar(h.Text)
		h.Text = strings.TrimSpace(text)
		if h.Text == "" {
			return
		}
		skriv(h)
	}
}

func korFraga(ctx context.Context, db *sql.DB, korare Korare, korning *Korning, prompt string, in FragaInput) {
	handelser, err := nyHandelseSkrivare(korning.Logg)
	if err != nil {
		avslutaFragaMedFel(ctx, db, korning, korare.Namn(), err, nil)
		return
	}
	handelser.Skriv(nyHandelse("text", "agenten bearbetar frågan"))
	res, korfel := korare.Kor(ctx, KorInput{
		Brief: prompt, Repo: korning.RepoPath, Logg: korning.Logg,
		Profil: in.Profil, PMBinar: in.PMBinar,
		Svarsfil:    filepath.Join(LoggKatalog(in.WorkspaceDir), korning.ID+".svar.txt"),
		VidHandelse: utanMinnesmarkor(handelser.Skriv),
	})
	if korfel == nil && res.ExitKod != 0 {
		korfel = fmt.Errorf("agenten %s avslutade med exitkod %d: %s", korare.Namn(), res.ExitKod, kortText(res.Utdata))
	}
	if korfel == nil && strings.TrimSpace(res.Utdata) == "" {
		korfel = fmt.Errorf("agenten %s gav ett tomt svar", korare.Namn())
	}
	if korfel != nil {
		avslutaFragaMedFel(ctx, db, korning, korare.Namn(), korfel, handelser)
		return
	}
	if err := handelser.Stang(); err != nil {
		avslutaFragaMedFel(ctx, db, korning, korare.Namn(), err, nil)
		return
	}
	text, minnesforslag := delaAgentsvar(res.Utdata)
	_, err = NewSamtalStore(db).AddMedKorning(ctx, korning.ProjectID, korning.ID,
		models.Actor{Kind: models.ActorKindAI, Name: korare.Namn()}, text, minnesforslag)
	if err != nil {
		avslutaFragaMedFel(ctx, db, korning, korare.Namn(), err, nil)
		return
	}
	_ = NewKorningStore(db).SattTokens(ctx, korning.ID, res.Tokens)
	_ = NewKorningStore(db).Avsluta(ctx, korning.ID, StatusKlar, res.ExitKod, korning.Logg)
}

func avslutaFragaMedFel(ctx context.Context, db *sql.DB, korning *Korning, agentnamn string, korfel error, handelser *handelseSkrivare) {
	text := "Frågan misslyckades: " + korfel.Error()
	if handelser != nil {
		handelser.Skriv(nyHandelse("fel", text))
		_ = handelser.Stang()
	}
	_, _ = NewSamtalStore(db).Add(ctx, korning.ProjectID, "",
		models.Actor{Kind: models.ActorKindAI, Name: agentnamn}, text)
	_ = NewKorningStore(db).Avsluta(ctx, korning.ID, StatusFel, 1, korning.Logg)
}

// Fraga sparar frågan i tråden, skickar den med projektets kontext till
// agenten och sparar svaret som ett ai-inlägg.
func Fraga(ctx context.Context, db *sql.DB, reg *AgentRegister, in FragaInput) (*Inlagg, error) {
	if reg == nil {
		return nil, fmt.Errorf("ingen agent är registrerad")
	}
	agent, err := reg.Hamta(in.Agent)
	if err != nil {
		return nil, err
	}
	store := NewSamtalStore(db)
	if _, err := store.Add(ctx, in.ProjectID, "", in.Fragare, in.Fraga); err != nil {
		return nil, err
	}
	kontext, err := ByggKontext(ctx, db, in.Alias, in.ProjectID, in.Granser)
	if err != nil {
		return nil, err
	}
	svar, err := agent.Fraga(ctx, ByggPrompt(kontext, in.Fraga))
	if err != nil {
		return nil, err
	}
	text, minnesforslag := delaAgentsvar(svar)
	return store.AddMedMinnesforslag(ctx, in.ProjectID, "",
		models.Actor{Kind: models.ActorKindAI, Name: agent.Namn()}, text, minnesforslag)
}

const minnesstart = "<pm-minne>"
const minnesslut = "</pm-minne>"

func delaAgentsvar(svar string) (string, string) {
	trimmat := strings.TrimSpace(svar)
	if !strings.HasSuffix(trimmat, minnesslut) {
		return trimmat, ""
	}
	start := strings.LastIndex(trimmat, minnesstart)
	if start < 0 {
		return trimmat, ""
	}
	minne := strings.TrimSpace(trimmat[start+len(minnesstart) : len(trimmat)-len(minnesslut)])
	return strings.TrimSpace(trimmat[:start]), minne
}
