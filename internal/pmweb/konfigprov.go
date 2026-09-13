package pmweb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/service"
)

type konfigProvDelresultat struct {
	Namn       string `json:"namn"`
	Status     string `json:"status"`
	Meddelande string `json:"meddelande"`
}

type konfigProvSvar struct {
	Agent       string                  `json:"agent"`
	Svar        string                  `json:"svar"`
	Delresultat []konfigProvDelresultat `json:"delresultat"`
}

func korKonfigprov(ctx context.Context, agentnamn string, agent pm.AgentKonfig) (konfigProvSvar, error) {
	ctx, avbryt := context.WithTimeout(ctx, provTimeout)
	defer avbryt()
	katalog, err := os.MkdirTemp("", "backlog-pm-prov-*")
	if err != nil {
		return konfigProvSvar{}, fmt.Errorf("kunde inte skapa provkatalogen: %w", err)
	}
	defer os.RemoveAll(katalog)
	// Codex vägrar arbeta utanför ett git-repo utan --skip-git-repo-check.
	// Provkatalogen får därför ett tomt repo, annars faller provet för codex
	// med ett fel som inte har med konfigurationen att göra.
	if err := gorProvkatalogTillRepo(ctx, katalog); err != nil {
		return konfigProvSvar{}, err
	}

	databas := filepath.Join(katalog, "backlog.db")
	db, err := repo.Open(databas)
	if err != nil {
		return konfigProvSvar{}, fmt.Errorf("kunde inte skapa provdatabasen: %w", err)
	}
	defer db.Close()
	if err := migrate.Run(db); err != nil {
		return konfigProvSvar{}, fmt.Errorf("kunde inte förbereda provdatabasen: %w", err)
	}
	if err := pm.Migrate(db); err != nil {
		return konfigProvSvar{}, fmt.Errorf("kunde inte förbereda PM-provet: %w", err)
	}

	aktor := models.Actor{Kind: models.ActorKindHuman, Name: "konfigprov"}
	projekt, err := service.NewProjectService(db).Create(ctx, models.CreateProjectInput{
		Alias: "konfigprov", Name: "Isolerat konfigprov", RepoPath: katalog, Actor: aktor,
	})
	if err != nil {
		return konfigProvSvar{}, fmt.Errorf("kunde inte skapa provprojektet: %w", err)
	}
	markor := "MCP-kontroll " + filepath.Base(katalog)
	task, err := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).Create(ctx, models.CreateTaskInput{
		ProjectID: projekt.ID, Title: markor, Actor: aktor,
	})
	if err != nil {
		return konfigProvSvar{}, fmt.Errorf("kunde inte skapa provtasken: %w", err)
	}

	agent.Miljo = kopieraMiljo(agent.Miljo)
	agent.Miljo["BACKLOG_DB"] = databas
	profil := profilNamn()
	if profil == "" {
		profil = "konfigprov"
	}
	svarsfil := filepath.Join(katalog, "svar.txt")
	brief := "Svara kort med texten: Konfigurationen fungerar."
	if agent.MCP {
		brief = fmt.Sprintf(`Detta är ett isolerat konfigprov. Använd MCP-servern backlog-pm.
Läs TASK-1 med verktyget task_show.
Skriv taskens exakta titel som kommentar på TASK-1 med verktyget comment_add.
Svara sedan kort med texten: Konfigurationen fungerar.`)
	}

	resultat, korfel := pm.NewKommandoAgent(agentnamn, agent).Kor(ctx, pm.KorInput{
		Brief: brief, Repo: katalog, Profil: profil, PMBinar: pm.PMBinar(), Svarsfil: svarsfil,
	})
	kommentarer, err := service.NewCommentService(db).ListForTask(ctx, task.ID)
	if err != nil {
		return konfigProvSvar{}, fmt.Errorf("kunde inte läsa MCP-provet: %w", err)
	}
	return byggKonfigProvSvar(agentnamn, agent, resultat, korfel, svarsfil, markor, kommentarer), nil
}

func kopieraMiljo(miljo map[string]string) map[string]string {
	kopia := make(map[string]string, len(miljo)+1)
	for namn, varde := range miljo {
		kopia[namn] = varde
	}
	return kopia
}

func byggKonfigProvSvar(
	agentnamn string,
	agent pm.AgentKonfig,
	resultat pm.Resultat,
	korfel error,
	svarsfil string,
	markor string,
	kommentarer []*models.Comment,
) konfigProvSvar {
	delar := make([]konfigProvDelresultat, 0, 5)
	startade := kommandotStartade(korfel)
	if !startade {
		delar = append(delar, provFel("Kommandot", fmt.Sprintf("Kommandot %q finns inte eller kunde inte starta.", agent.Kommando)))
	} else if korfel != nil {
		delar = append(delar, provFel("Kommandot", "Kommandot startade men PM kunde inte slutföra körningen."))
	} else if resultat.ExitKod != 0 {
		// En nollskild exitkod betyder att agenten själv sa att något gick fel.
		delar = append(delar, provFel("Kommandot", fmt.Sprintf("Kommandot startade men avslutades med exitkod %d.", resultat.ExitKod)))
	} else {
		delar = append(delar, provOK("Kommandot", "Kommandot startade och avslutades utan fel."))
	}

	svar := strings.TrimSpace(resultat.Utdata)
	if svar != "" {
		delar = append(delar, provOK("Agentsvar", "Agenten lämnade ett svar."))
	} else if startade {
		delar = append(delar, provFel("Agentsvar", "Agenten lämnade inget svar."))
	} else {
		delar = append(delar, provFel("Agentsvar", "Agenten kunde inte svara eftersom kommandot inte startade."))
	}

	matchande := hittaProvkommentar(markor, kommentarer)
	if !agent.MCP {
		delar = append(delar, provOverhoppad("MCP", "MCP-provet hoppades över eftersom agenten saknar MCP."))
		delar = append(delar, provOverhoppad("Aktör", "Aktörsprovet hoppades över tillsammans med MCP-provet."))
	} else if matchande == nil {
		delar = append(delar, provFel("MCP", "Agenten skrev inte provkommentaren via MCP."))
		delar = append(delar, provFel("Aktör", "Aktören kunde inte kontrolleras eftersom provkommentaren saknas."))
	} else {
		delar = append(delar, provOK("MCP", "Agenten läste tasken och skrev provkommentaren via MCP."))
		if matchande.Actor.Kind == models.ActorKindAI && matchande.Actor.Name == agentnamn {
			delar = append(delar, provOK("Aktör", fmt.Sprintf("Provkommentaren skrevs som ai:%s.", agentnamn)))
		} else {
			delar = append(delar, provFel("Aktör", fmt.Sprintf("Provkommentaren skrevs som %s:%s.", matchande.Actor.Kind, matchande.Actor.Name)))
		}
	}

	if agent.Svar != "fil" {
		delar = append(delar, provOverhoppad("Svarsfil", "Svarsfilen behövs inte när agenten svarar via standardutdata."))
	} else if data, err := os.ReadFile(svarsfil); err != nil {
		delar = append(delar, provFel("Svarsfil", "Agenten skapade ingen svarsfil."))
	} else if strings.TrimSpace(string(data)) == "" {
		delar = append(delar, provFel("Svarsfil", "Agenten skapade svarsfilen men lämnade den tom."))
	} else {
		delar = append(delar, provOK("Svarsfil", "Agenten skrev sitt svar i svarsfilen."))
	}
	return konfigProvSvar{Agent: agentnamn, Svar: svar, Delresultat: delar}
}

func kommandotStartade(err error) bool {
	if err == nil {
		return true
	}
	var execFel *exec.Error
	var sokvagsfel *os.PathError
	return !errors.As(err, &execFel) && !errors.As(err, &sokvagsfel)
}

func hittaProvkommentar(markor string, kommentarer []*models.Comment) *models.Comment {
	for _, kommentar := range kommentarer {
		if strings.Contains(kommentar.Body, markor) {
			return kommentar
		}
	}
	return nil
}

func provOK(namn, meddelande string) konfigProvDelresultat {
	return konfigProvDelresultat{Namn: namn, Status: "ok", Meddelande: meddelande}
}

func provFel(namn, meddelande string) konfigProvDelresultat {
	return konfigProvDelresultat{Namn: namn, Status: "fel", Meddelande: meddelande}
}

func provOverhoppad(namn, meddelande string) konfigProvDelresultat {
	return konfigProvDelresultat{Namn: namn, Status: "overhoppad", Meddelande: meddelande}
}

// gorProvkatalogTillRepo skapar ett tomt git-repo i provkatalogen. Saknas git
// på maskinen går provet vidare ändå, för alla agenter kräver inte ett repo.
func gorProvkatalogTillRepo(ctx context.Context, katalog string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	kommando := exec.CommandContext(ctx, "git", "init", "--quiet", katalog)
	if ut, err := kommando.CombinedOutput(); err != nil {
		return fmt.Errorf("kunde inte förbereda provkatalogen som git-repo: %s", strings.TrimSpace(string(ut)))
	}
	return nil
}
