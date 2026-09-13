package pmweb

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/service"
)

type oversiktsProjekt struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`
	Namn  string `json:"namn"`
}

type oversiktsKorning struct {
	pm.Korning
	Projekt oversiktsProjekt `json:"projekt"`
	Koorsak string           `json:"koorsak,omitempty"`
}

type oversiktsVantar struct {
	Sort      string           `json:"sort"`
	Ref       string           `json:"ref,omitempty"`
	TaskID    string           `json:"task_id,omitempty"`
	Text      string           `json:"text"`
	KorningID string           `json:"korning_id,omitempty"`
	InlaggID  string           `json:"inlagg_id,omitempty"`
	Projekt   oversiktsProjekt `json:"projekt"`
}

type oversiktsTestserver struct {
	testserverSvar
	Projekt oversiktsProjekt `json:"projekt"`
	Fel     string           `json:"fel,omitempty"`
}

type allaOversikt struct {
	Projekt     []oversiktsProjekt    `json:"projekt"`
	Korningar   []oversiktsKorning    `json:"korningar"`
	Vantar      []oversiktsVantar     `json:"vantar"`
	Testservrar []oversiktsTestserver `json:"testservrar"`
}

// hamtaAllaOversikt ger bara nuläget. Avslutad körningshistorik hör till
// projektets paginerade körningsrutt och pollas därför inte här.
func (s *Server) hamtaAllaOversikt(w http.ResponseWriter, r *http.Request) {
	svar, err := s.byggAllaOversikt(r.Context())
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, svar)
}

func (s *Server) byggAllaOversikt(ctx context.Context) (*allaOversikt, error) {
	projekt, err := service.NewProjectService(s.db).List(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("läs projekt till översikten: %w", err)
	}
	svar := &allaOversikt{
		Projekt: []oversiktsProjekt{}, Korningar: []oversiktsKorning{},
		Vantar: []oversiktsVantar{}, Testservrar: []oversiktsTestserver{},
	}
	projektID := make(map[string]oversiktsProjekt, len(projekt))
	for _, p := range projekt {
		rad := oversiktsProjekt{ID: p.ID, Alias: p.Alias, Namn: p.Name}
		svar.Projekt = append(svar.Projekt, rad)
		projektID[p.ID] = rad
	}

	aktiva, err := pm.NewKorningStore(s.db).ListaFiltrerad(ctx, pm.KorningFilter{Status: "pagaende"})
	if err != nil {
		return nil, err
	}
	maxSamtidiga := 0
	if konfig, konfigfel := pm.LasKonfig(konfigWorkDir()); konfigfel == nil {
		maxSamtidiga = konfig.MaxSamtidiga
	}
	for _, k := range aktiva {
		p, finns := projektID[k.ProjectID]
		if !finns {
			continue
		}
		rad := oversiktsKorning{Korning: k, Projekt: p}
		if k.Status == pm.StatusKoad {
			rad.Koorsak = globalKoorsak(k, aktiva, maxSamtidiga)
		}
		svar.Korningar = append(svar.Korningar, rad)
	}

	vantar, err := hamtaGlobaltVantar(ctx, s.db)
	if err != nil {
		return nil, err
	}
	svar.Vantar = vantar
	svar.Testservrar = s.hamtaAllaTestservrar(ctx, projekt)
	return svar, nil
}

func globalKoorsak(k pm.Korning, aktiva []pm.Korning, maxSamtidiga int) string {
	kor := 0
	for _, annan := range aktiva {
		if annan.Status != pm.StatusKor {
			continue
		}
		kor++
		if k.RepoPath != "" && annan.RepoPath == k.RepoPath {
			return "Repot används av en annan körning."
		}
	}
	if maxSamtidiga > 0 && kor >= maxSamtidiga {
		return fmt.Sprintf("Alla %d agentplatser används.", maxSamtidiga)
	}
	return "Väntar på en ledig agentplats."
}

func (s *Server) hamtaAllaTestservrar(ctx context.Context, projekt []*models.Project) []oversiktsTestserver {
	svar := make([]oversiktsTestserver, 0, len(projekt))
	store, konfig, err := s.testserverStore()
	for _, p := range projekt {
		projektrad := oversiktsProjekt{ID: p.ID, Alias: p.Alias, Namn: p.Name}
		if err != nil {
			svar = append(svar, oversiktsTestserver{
				testserverSvar: testserverSvar{Alias: p.Alias}, Projekt: projektrad,
				Fel: "Testserverns status kunde inte läsas.",
			})
			continue
		}
		server, statusfel := store.Status(ctx, p.Alias)
		if statusfel != nil {
			svar = append(svar, oversiktsTestserver{
				testserverSvar: testserverSvar{Alias: p.Alias}, Projekt: projektrad,
				Fel: "Testserverns status kunde inte läsas.",
			})
			continue
		}
		svar = append(svar, oversiktsTestserver{
			testserverSvar: testserverTillSvar(p.Alias, server, konfig), Projekt: projektrad,
		})
	}
	return svar
}

func hamtaGlobaltVantar(ctx context.Context, db *sql.DB) ([]oversiktsVantar, error) {
	svar := []oversiktsVantar{}
	fragor := []struct {
		query string
		scan  func(*sql.Rows) (oversiktsVantar, error)
	}{
		{query: `SELECT p.id,p.alias,p.name,k.task_ref,t.id,k.id,k.agent,k.exit_kod
			FROM pm_korningar k JOIN tasks t ON t.id=k.task_id JOIN projects p ON p.id=k.project_id
			WHERE p.archived_at IS NULL AND t.archived_at IS NULL AND t.status<>'done' AND k.status='fel'
			AND NOT EXISTS (SELECT 1 FROM pm_korningar ny WHERE ny.task_id=k.task_id
				AND (ny.skapad_at>k.skapad_at OR (ny.skapad_at=k.skapad_at AND ny.id>k.id)))
			ORDER BY k.skapad_at DESC`, scan: skannaVantandeFel},
		{query: `SELECT p.id,p.alias,p.name,s.id,s.actor_name,s.text
			FROM pm_samtal s JOIN projects p ON p.id=s.project_id
			WHERE p.archived_at IS NULL AND s.actor_kind='ai' AND s.kvitterad_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM pm_samtal ny WHERE ny.project_id=s.project_id
				AND (ny.created_at>s.created_at OR (ny.created_at=s.created_at AND ny.id>s.id)))
			ORDER BY s.created_at DESC`, scan: skannaVantandeFraga},
		{query: `SELECT p.id,p.alias,p.name,t.id,t.task_seq,t.priority,t.title
			FROM tasks t JOIN projects p ON p.id=t.project_id
			WHERE p.archived_at IS NULL AND t.archived_at IS NULL AND t.status='todo' AND t.priority<=2
			AND NOT EXISTS (SELECT 1 FROM pm_korningar k WHERE k.task_id=t.id)
			ORDER BY t.priority,t.created_at`, scan: skannaVantandeBeslut},
	}
	for _, fraga := range fragor {
		rader, err := db.QueryContext(ctx, fraga.query)
		if err != nil {
			return nil, fmt.Errorf("läs det som väntar: %w", err)
		}
		for rader.Next() {
			rad, scanfel := fraga.scan(rader)
			if scanfel != nil {
				rader.Close()
				return nil, scanfel
			}
			svar = append(svar, rad)
		}
		if err := rader.Err(); err != nil {
			rader.Close()
			return nil, err
		}
		rader.Close()
	}
	return svar, nil
}

func skannaVantandeFel(rader *sql.Rows) (oversiktsVantar, error) {
	var v oversiktsVantar
	var agent string
	var exitkod sql.NullInt64
	if err := rader.Scan(&v.Projekt.ID, &v.Projekt.Alias, &v.Projekt.Namn, &v.Ref, &v.TaskID, &v.KorningID, &agent, &exitkod); err != nil {
		return v, err
	}
	kod := "-"
	if exitkod.Valid {
		kod = fmt.Sprintf("%d", exitkod.Int64)
	}
	v.Sort = "fel"
	v.Text = fmt.Sprintf("Körningen misslyckades (%s, exitkod %s)", agent, kod)
	return v, nil
}

func skannaVantandeFraga(rader *sql.Rows) (oversiktsVantar, error) {
	var v oversiktsVantar
	var agent, text string
	if err := rader.Scan(&v.Projekt.ID, &v.Projekt.Alias, &v.Projekt.Namn, &v.InlaggID, &agent, &text); err != nil {
		return v, err
	}
	v.Sort = "fraga"
	v.Text = fmt.Sprintf("%s väntar på svar i samtalet: %s", agent, kort(text, 160))
	return v, nil
}

func skannaVantandeBeslut(rader *sql.Rows) (oversiktsVantar, error) {
	var v oversiktsVantar
	var seq, prioritet int
	var titel string
	if err := rader.Scan(&v.Projekt.ID, &v.Projekt.Alias, &v.Projekt.Namn, &v.TaskID, &seq, &prioritet, &titel); err != nil {
		return v, err
	}
	v.Sort = "beslut"
	v.Ref = fmt.Sprintf("TASK-%d", seq)
	v.Text = fmt.Sprintf("P%d utan utdelning: %s", prioritet, titel)
	return v, nil
}
