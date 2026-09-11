package pm

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/service"
)

// UtdelInput är en utdelning av en task till en agent.
type UtdelInput struct {
	TaskID       string
	Overstyrning string // --agent
	WorkspaceDir string
	Profil       string
	PMBinar      string
	KoTimeout    time.Duration
	Neka         bool // neka i stället för att köa när repot är upptaget
}

// Utdelare kör en task via en agent och för tillbaka resultatet till tavlan.
type Utdelare struct {
	db       *sql.DB
	konfig   Konfig
	register *AgentRegister
	// Nu och Vanta gör kösemantiken testbar.
	Vanteintervall time.Duration
}

func NewUtdelare(db *sql.DB, konfig Konfig, register *AgentRegister) *Utdelare {
	return &Utdelare{db: db, konfig: konfig, register: register, Vanteintervall: 500 * time.Millisecond}
}

// DelaUt väljer agent, köar mot repo-låset, kör agenten och skriver tillbaka
// resultatet som kommentar och status.
func (u *Utdelare) DelaUt(ctx context.Context, in UtdelInput) (*Korning, error) {
	store := NewKorningStore(u.db)
	if _, err := store.StadaOvergivna(ctx); err != nil {
		return nil, err
	}

	fakta, err := HamtaTaskFakta(ctx, u.db, in.TaskID)
	if err != nil {
		return nil, err
	}
	val, err := ValjAgent(u.konfig, fakta, in.Overstyrning)
	if err != nil {
		return nil, err
	}
	korare, err := u.register.HamtaKorare(val.Agent)
	if err != nil {
		return nil, err
	}

	korning := &Korning{
		ProjectID: fakta.ProjectID, TaskID: fakta.ID, TaskRef: fakta.Ref,
		Agent: val.Agent, Motivering: val.Motivering, RepoPath: fakta.RepoPath, Status: StatusKoad,
	}
	if err := store.Skapa(ctx, korning); err != nil {
		return nil, err
	}
	korning.Logg = filepath.Join(LoggKatalog(in.WorkspaceDir), korning.ID+".log")
	if err := os.MkdirAll(LoggKatalog(in.WorkspaceDir), 0o755); err != nil {
		return nil, err
	}

	las := NyRepoLas(in.WorkspaceDir, fakta.RepoPath)
	tagen, err := las.Ta(fakta.RepoPath, korning.ID)
	if err != nil {
		return nil, err
	}
	if !tagen {
		if in.Neka {
			return korning, fmt.Errorf("repot är upptaget: %s. Kör igen senare, eller vänta i kön utan --neka", las.LasBesked())
		}
		timeout := in.KoTimeout
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		tagen, err = las.VantaOchTa(fakta.RepoPath, korning.ID, timeout, u.Vanteintervall)
		if err != nil {
			return nil, err
		}
		if !tagen {
			_ = store.Avsluta(ctx, korning.ID, StatusFel, 1, korning.Logg)
			return korning, fmt.Errorf("repot %s var upptaget hela kötiden: %s", fakta.RepoPath, las.LasBesked())
		}
	}
	defer las.Slapp()

	u.krok(ctx, u.konfig.Krok.Anspraka, fakta, korning, in)
	defer u.krok(context.WithoutCancel(ctx), u.konfig.Krok.Slapp, fakta, korning, in)

	return u.kor(ctx, store, korare, fakta, korning, in)
}

func (u *Utdelare) kor(ctx context.Context, store *KorningStore, korare Korare, fakta TaskFakta, korning *Korning, in UtdelInput) (*Korning, error) {
	if err := store.SattStatus(ctx, korning.ID, StatusKor); err != nil {
		return korning, err
	}
	korning.Status = StatusKor

	tasks := service.NewTaskService(u.db, service.NewPlanService(u.db), service.NewLabelService(u.db))
	agentAktor := models.Actor{Kind: models.ActorKindAI, Name: korare.Namn()}
	if _, err := tasks.Move(ctx, fakta.ID, models.TaskStatus("doing"), agentAktor); err != nil {
		return korning, fmt.Errorf("kunde inte flytta tasken till doing: %w", err)
	}

	brief, err := ByggBrief(ctx, u.db, fakta)
	if err != nil {
		return korning, err
	}

	var res Resultat
	var korfel error
	handelser, err := nyHandelseSkrivare(korning.Logg)
	if err != nil {
		res = Resultat{ExitKod: 1, Logg: korning.Logg}
		korfel = fmt.Errorf("kunde inte skapa händelseloggen: %w", err)
	} else {
		res, korfel = korare.Kor(ctx, KorInput{
			Brief: brief, Repo: fakta.RepoPath, Logg: korning.Logg,
			Profil: in.Profil, TaskRef: fakta.Ref, PMBinar: in.PMBinar,
			Svarsfil:    filepath.Join(LoggKatalog(in.WorkspaceDir), korning.ID+".svar.txt"),
			VidHandelse: handelser.Skriv,
		})
		if err := handelser.Stang(); err != nil && korfel == nil {
			korfel = fmt.Errorf("kunde inte skriva händelseloggen: %w", err)
		}
	}

	status := StatusKlar
	nyStatus := models.TaskStatus("done")
	if korfel != nil || res.ExitKod != 0 {
		status = StatusFel
		nyStatus = models.TaskStatus("todo")
	}
	if korfel != nil && res.ExitKod == 0 {
		res.ExitKod = 1
	}

	u.skrivKommentar(ctx, fakta, korning, res, korfel, agentAktor)

	if _, err := tasks.Move(ctx, fakta.ID, nyStatus, agentAktor); err != nil {
		return korning, fmt.Errorf("kunde inte flytta tasken till %s: %w", nyStatus, err)
	}
	if err := store.Avsluta(ctx, korning.ID, status, res.ExitKod, korning.Logg); err != nil {
		return korning, err
	}
	korning.Status = status
	kod := res.ExitKod
	korning.ExitKod = &kod
	return korning, nil
}

func (u *Utdelare) skrivKommentar(ctx context.Context, fakta TaskFakta, korning *Korning, res Resultat, korfel error, aktor models.Actor) {
	kommentarer := service.NewCommentService(u.db)
	var b strings.Builder
	if res.ExitKod == 0 && korfel == nil {
		fmt.Fprintf(&b, "Utdelad körning klar (agent %s, exitkod 0).\n\n%s\n", korning.Agent, strings.TrimSpace(res.Utdata))
	} else {
		fmt.Fprintf(&b, "Utdelad körning misslyckades (agent %s, exitkod %d). Tasken är tillbaka i todo.\n",
			korning.Agent, res.ExitKod)
		if korfel != nil {
			fmt.Fprintf(&b, "\nFel: %v\n", korfel)
		}
		if utdata := strings.TrimSpace(res.Utdata); utdata != "" {
			fmt.Fprintf(&b, "\n%s\n", kortText(utdata))
		}
	}
	fmt.Fprintf(&b, "\nKörning %s, vald agent: %s. Logg: %s", korning.ID, korning.Motivering, korning.Logg)

	if _, err := kommentarer.Create(ctx, models.CreateCommentInput{TaskID: fakta.ID, Body: b.String(), Actor: aktor}); err != nil {
		fmt.Fprintf(os.Stderr, "kunde inte skriva kommentar på %s: %v\n", fakta.Ref, err)
	}
}

// krok kör ett valfritt externt anspråkskommando, t.ex. arbetar. Saknas kroken
// händer ingenting och kön fungerar ändå.
func (u *Utdelare) krok(ctx context.Context, mall []string, fakta TaskFakta, korning *Korning, in UtdelInput) {
	if len(mall) == 0 {
		return
	}
	platshallare := KorInput{Repo: fakta.RepoPath, TaskRef: fakta.Ref, Logg: korning.Logg, Profil: in.Profil}
	args := make([]string, 0, len(mall))
	// {agent} är agentens namn. {aktor} är samma sak som aktörssträng, så
	// kroken skriver i agentens namn och inte som en människa.
	aktor := "ai:" + korning.Agent
	for _, del := range mall {
		del = ersattPlatshallare(del, platshallare)
		del = strings.ReplaceAll(del, "{aktor}", aktor)
		args = append(args, strings.ReplaceAll(del, "{agent}", korning.Agent))
	}
	ctx, avbryt := context.WithTimeout(ctx, 30*time.Second)
	defer avbryt()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = nil
	cmd.Env = krokMiljo(u.konfig.Krok.Miljo, in)
	if ut, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "kroken %s misslyckades: %v: %s\n", args[0], err, strings.TrimSpace(string(ut)))
	}
}

// krokMiljo pekar kroken mot PM-workspacet. Utan det kör ett backlog-baserat
// verktyg som arbetar mot vardagsdatabasen och kan skriva på fel task.
func krokMiljo(extra map[string]string, in UtdelInput) []string {
	miljo := os.Environ()
	if in.Profil != "" {
		miljo = append(miljo, "BACKLOG_PROFILE="+in.Profil)
	}
	if in.WorkspaceDir != "" {
		miljo = append(miljo, "BACKLOG_DB="+filepath.Join(in.WorkspaceDir, "backlog.db"))
	}
	for k, v := range extra {
		miljo = append(miljo, k+"="+v)
	}
	return miljo
}

// LoggKatalog pekar ut var körningarna skriver sina loggar.
func LoggKatalog(workspaceDir string) string { return filepath.Join(workspaceDir, "korningar") }
