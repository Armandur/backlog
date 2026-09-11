package pmweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/service"
)

const (
	forslagKlassning = "klassning"
	forslagBerikning = "berikning"
)

var taskForslagTimeout = provTimeout

type foreslaTaskBody struct {
	Sort  string `json:"sort"`
	Agent string `json:"agent"`
}

type klassningsforslag struct {
	Typ          models.TaskType `json:"typ"`
	Prioritet    int             `json:"prioritet"`
	Modell       string          `json:"modell"`
	Anstrangning string          `json:"anstrangning"`
}

type berikningsforslag struct {
	Titel       string `json:"titel"`
	Beskrivning string `json:"beskrivning"`
}

type taskUtkast struct {
	Sort         string          `json:"sort"`
	Ref          string          `json:"ref"`
	Titel        string          `json:"titel"`
	Beskrivning  string          `json:"beskrivning"`
	Typ          models.TaskType `json:"typ"`
	Prioritet    int             `json:"prioritet"`
	Modell       string          `json:"modell"`
	Anstrangning string          `json:"anstrangning"`
}

type uppdateraTaskBody struct {
	Titel       *string          `json:"titel"`
	Beskrivning *string          `json:"beskrivning"`
	Typ         *models.TaskType `json:"typ"`
	Prioritet   *int             `json:"prioritet"`
}

func (s *Server) foreslaTask(w http.ResponseWriter, r *http.Request) {
	var body foreslaTaskBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa vilket förslag du vill ha"), http.StatusBadRequest)
		return
	}
	body.Sort = strings.TrimSpace(body.Sort)
	if body.Sort != forslagKlassning && body.Sort != forslagBerikning {
		svaraFel(w, errors.New("välj klassning eller berikning"), http.StatusBadRequest)
		return
	}

	tasks := nyTaskService(s)
	task, err := tasks.Get(r.Context(), r.PathValue("id"), false, false)
	if err != nil {
		svaraFel(w, errors.New("tasken finns inte"), http.StatusNotFound)
		return
	}
	agent, err := s.aktuelltRegister().Hamta(strings.TrimSpace(body.Agent))
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}

	ctx, avbryt := context.WithTimeout(r.Context(), taskForslagTimeout)
	defer avbryt()
	svar, err := agent.Fraga(ctx, byggTaskForslagsprompt(body.Sort, task))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			svaraFel(w, errors.New("agenten hann inte skapa ett förslag"), http.StatusGatewayTimeout)
			return
		}
		svaraFel(w, fmt.Errorf("agenten kunde inte skapa förslaget: %w", err), http.StatusBadGateway)
		return
	}

	utkast := taskUtkast{
		Sort: body.Sort, Ref: fmt.Sprintf("TASK-%d", task.Seq), Titel: task.Title,
		Beskrivning: task.Description, Typ: task.Type, Prioritet: task.Priority,
	}
	if err := tolkaTaskForslag(strings.TrimSpace(svar), &utkast); err != nil {
		meddelande, kod := begripligtTaskfel(err)
		if errors.Is(err, service.ErrTaskDescRequired) {
			meddelande = "agentens förslag saknar en beskrivning"
		} else if kod == http.StatusInternalServerError {
			meddelande = err.Error()
		}
		svaraFel(w, errors.New(meddelande), http.StatusBadGateway)
		return
	}
	svaraJSON(w, http.StatusOK, utkast)
}

func tolkaTaskForslag(svar string, utkast *taskUtkast) error {
	if utkast.Sort == forslagKlassning {
		var forslag klassningsforslag
		if err := json.Unmarshal([]byte(svar), &forslag); err != nil {
			return errors.New("agenten svarade inte med giltig JSON")
		}
		if err := service.ValidateTaskType(forslag.Typ); err != nil {
			return err
		}
		if err := service.ValidateTaskPriority(forslag.Prioritet); err != nil {
			return err
		}
		utkast.Typ = forslag.Typ
		utkast.Prioritet = forslag.Prioritet
		utkast.Modell = strings.TrimSpace(forslag.Modell)
		utkast.Anstrangning = strings.TrimSpace(forslag.Anstrangning)
		return nil
	}

	var forslag berikningsforslag
	if err := json.Unmarshal([]byte(svar), &forslag); err != nil {
		return errors.New("agenten svarade inte med giltig JSON")
	}
	forslag.Titel = strings.TrimSpace(forslag.Titel)
	forslag.Beskrivning = strings.TrimSpace(forslag.Beskrivning)
	if err := service.ValidateTaskTitle(forslag.Titel); err != nil {
		return err
	}
	if forslag.Beskrivning == "" {
		return service.ErrTaskDescRequired
	}
	if err := service.ValidateTaskDescription(forslag.Beskrivning); err != nil {
		return err
	}
	utkast.Titel = forslag.Titel
	utkast.Beskrivning = forslag.Beskrivning
	return nil
}

func byggTaskForslagsprompt(sort string, task *models.Task) string {
	if sort == forslagKlassning {
		typer := make([]string, 0, len(models.AllTaskTypes()))
		for _, taskTyp := range models.AllTaskTypes() {
			typer = append(typer, string(taskTyp))
		}
		return fmt.Sprintf(`Klassificera tasken utifrån titeln och beskrivningen.
Svara endast med ett JSON-objekt utan kodstaket eller förklaringar.
Objektet ska ha fälten typ, prioritet, modell och anstrangning.
Typ måste vara ett av följande värden: %s.
Prioritet måste vara ett heltal från 1 till 5.
Modell och anstrangning är valfria. Skriv en tom sträng när du saknar en tydlig åsikt.

Titel: %s
Beskrivning:
%s`, strings.Join(typer, ", "), task.Title, task.Description)
	}
	return fmt.Sprintf(`Berika tasken utifrån den nuvarande titeln och beskrivningen.
Svara endast med ett JSON-objekt utan kodstaket eller förklaringar.
Objektet ska ha fälten titel och beskrivning.
Skriv på svenska. Beskrivningen ska ha rubrikerna Kontext, Acceptanskriterier och Verifiering.

Nuvarande titel: %s
Nuvarande beskrivning:
%s`, task.Title, task.Description)
}

func (s *Server) uppdateraTask(w http.ResponseWriter, r *http.Request) {
	var body uppdateraTaskBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa taskens uppgifter"), http.StatusBadRequest)
		return
	}
	if body.Titel != nil {
		*body.Titel = strings.TrimSpace(*body.Titel)
	}
	if body.Beskrivning != nil {
		*body.Beskrivning = strings.TrimSpace(*body.Beskrivning)
	}
	tasks := nyTaskService(s)
	if _, err := tasks.Get(r.Context(), r.PathValue("id"), false, false); err != nil {
		svaraFel(w, errors.New("tasken finns inte"), http.StatusNotFound)
		return
	}
	task, err := tasks.Update(r.Context(), r.PathValue("id"), models.UpdateTaskInput{
		Title: body.Titel, Description: body.Beskrivning, Type: body.Typ, Priority: body.Prioritet,
	}, s.aktor)
	if err != nil {
		meddelande, kod := begripligtTaskfel(err)
		svaraFel(w, errors.New(meddelande), kod)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{
		"ref": fmt.Sprintf("TASK-%d", task.Seq), "titel": task.Title,
		"beskrivning": task.Description, "typ": task.Type, "prioritet": task.Priority,
	})
}

func nyTaskService(s *Server) *service.TaskService {
	return service.NewTaskService(s.db, service.NewPlanService(s.db), service.NewLabelService(s.db))
}
