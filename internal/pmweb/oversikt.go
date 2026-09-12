package pmweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
	"github.com/mazen160/backlog/internal/service"
)

// Oversikt är projektvyns underlag: pågående körningar, tasks, det som väntar
// på Rasmus och det som är blockerat.
type Oversikt struct {
	Projekt   *models.Project `json:"projekt"`
	Korningar []pm.Korning    `json:"korningar"`
	Tasks     []TaskRad       `json:"tasks"`
	Vantar    []VantarPost    `json:"vantar"`
	Blockerat []TaskRad       `json:"blockerat"`
}

// TaskRad är en task i projektvyn med den senaste körningen bredvid.
type TaskRad struct {
	ID           string      `json:"id"`
	Ref          string      `json:"ref"`
	Titel        string      `json:"titel"`
	Typ          string      `json:"typ"`
	Status       string      `json:"status"`
	Prioritet    int         `json:"prioritet"`
	Etiketter    []string    `json:"etiketter"`
	SistaKorning *pm.Korning `json:"sista_korning,omitempty"`
	Skal         string      `json:"skal,omitempty"`
}

// VantarPost är en rad under "väntar på mig".
type VantarPost struct {
	Sort      string `json:"sort"` // fel, fraga, beslut
	Ref       string `json:"ref,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Text      string `json:"text"`
	KorningID string `json:"korning_id,omitempty"`
	InlaggID  string `json:"inlagg_id,omitempty"`
}

func (s *Server) skapaTask(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")
	projekt, err := service.NewProjectService(s.db).GetByAlias(r.Context(), alias)
	if err != nil {
		svaraFel(w, fmt.Errorf("projektet %q finns inte i PM-workspacet", alias), http.StatusNotFound)
		return
	}

	var body skapaTaskBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa taskens uppgifter"), http.StatusBadRequest)
		return
	}
	uppgift, err := service.NewTaskService(s.db, service.NewPlanService(s.db), service.NewLabelService(s.db)).Create(
		r.Context(), models.CreateTaskInput{
			ProjectID: projekt.ID,
			Title:     strings.TrimSpace(body.Titel), Description: strings.TrimSpace(body.Beskrivning),
			Type: body.Typ, Priority: body.Prioritet, Actor: s.aktor,
		},
	)
	if err != nil {
		meddelande, kod := begripligtTaskfel(err)
		svaraFel(w, errors.New(meddelande), kod)
		return
	}
	svaraJSON(w, http.StatusCreated, map[string]any{
		"ref":   fmt.Sprintf("TASK-%d", uppgift.Seq),
		"titel": uppgift.Title,
	})
}

func begripligtTaskfel(err error) (string, int) {
	switch {
	case errors.Is(err, service.ErrTaskTitleRequired):
		return "ange taskens titel", http.StatusBadRequest
	case errors.Is(err, service.ErrTaskTitleTooLong):
		return "taskens titel får innehålla högst 255 tecken", http.StatusBadRequest
	case errors.Is(err, service.ErrTaskDescTooLong):
		return "beskrivningen får innehålla högst 65535 tecken", http.StatusBadRequest
	case errors.Is(err, service.ErrTaskTypeInvalid):
		return "välj en giltig typ", http.StatusBadRequest
	case errors.Is(err, service.ErrTaskStatusInvalid):
		return "välj en giltig status", http.StatusBadRequest
	case errors.Is(err, service.ErrTaskPriority):
		return "prioriteten måste vara P1-P5", http.StatusBadRequest
	default:
		return "PM kunde inte lägga till tasken", http.StatusInternalServerError
	}
}

// byggOversikt samlar allt projektvyn visar.
func byggOversikt(ctx context.Context, db *sql.DB, alias string) (*Oversikt, error) {
	projekt, err := service.NewProjectService(db).GetByAlias(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("projektet %q finns inte i PM-workspacet", alias)
	}

	tasks, _, err := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db)).
		List(ctx, models.TaskFilter{ProjectAlias: alias, Sort: "priority"})
	if err != nil {
		return nil, err
	}

	korningar, err := pm.NewKorningStore(db).Lista(ctx, projekt.ID, 0)
	if err != nil {
		return nil, err
	}

	o := &Oversikt{Projekt: projekt, Korningar: []pm.Korning{}, Tasks: []TaskRad{}, Vantar: []VantarPost{}, Blockerat: []TaskRad{}}
	for _, k := range korningar {
		if k.Status == pm.StatusKoad || k.Status == pm.StatusKor {
			o.Korningar = append(o.Korningar, k)
		}
	}

	sista := map[string]*pm.Korning{}
	for i := range korningar {
		k := korningar[i]
		if _, finns := sista[k.TaskID]; !finns {
			sista[k.TaskID] = &k
		}
	}

	for _, t := range tasks {
		rad := TaskRad{
			ID: t.ID, Ref: fmt.Sprintf("TASK-%d", t.Seq), Titel: t.Title,
			Typ: string(t.Type), Status: string(t.Status), Prioritet: t.Priority,
			Etiketter: etikettnamn(t.Labels), SistaKorning: sista[t.ID],
		}
		o.Tasks = append(o.Tasks, rad)

		if skal := blockeringsskal(rad); skal != "" {
			rad.Skal = skal
			o.Blockerat = append(o.Blockerat, rad)
		}
		if k := rad.SistaKorning; k != nil && k.Status == pm.StatusFel && t.Status != models.TaskStatus("done") {
			o.Vantar = append(o.Vantar, VantarPost{
				Sort: "fel", Ref: rad.Ref, TaskID: t.ID, KorningID: k.ID,
				Text: fmt.Sprintf("Körningen misslyckades (%s, exitkod %s)", k.Agent, exitText(k.ExitKod)),
			})
		}
	}

	fraga, err := obesvaradAgentfraga(ctx, db, projekt.ID)
	if err != nil {
		return nil, err
	}
	if fraga != nil {
		o.Vantar = append(o.Vantar, *fraga)
	}

	for _, rad := range o.Tasks {
		if rad.Status == "todo" && rad.Prioritet <= 2 && rad.SistaKorning == nil {
			o.Vantar = append(o.Vantar, VantarPost{
				Sort: "beslut", Ref: rad.Ref, TaskID: rad.ID,
				Text: fmt.Sprintf("P%d utan utdelning: %s", rad.Prioritet, rad.Titel),
			})
		}
	}
	return o, nil
}

// blockeringsskal: etiketten blockerad, eller loopens egen markering i en
// kommentar ("Blockerad efter N försök").
func blockeringsskal(rad TaskRad) string {
	for _, e := range rad.Etiketter {
		if strings.EqualFold(e, "blockerad") || strings.EqualFold(e, "blocked") {
			return "etiketten " + e
		}
	}
	return ""
}

// obesvaradAgentfraga är sant när sista inlägget i tråden kommer från en agent.
func obesvaradAgentfraga(ctx context.Context, db *sql.DB, projectID string) (*VantarPost, error) {
	poster, err := pm.NewSamtalStore(db).List(ctx, projectID, 1)
	if err != nil {
		return nil, err
	}
	if len(poster) == 0 {
		return nil, nil
	}
	sista := poster[len(poster)-1]
	if sista.Actor.Kind != models.ActorKindAI || sista.KvitteradAt != nil {
		return nil, nil
	}
	return &VantarPost{
		Sort: "fraga", InlaggID: sista.ID,
		Text: fmt.Sprintf("%s väntar på svar i samtalet: %s", sista.Actor.Name, kort(sista.Text, 160)),
	}, nil
}

func etikettnamn(labels []models.Label) []string {
	namn := []string{}
	for _, l := range labels {
		namn = append(namn, l.Name)
	}
	return namn
}

func exitText(kod *int) string {
	if kod == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *kod)
}

func kort(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
