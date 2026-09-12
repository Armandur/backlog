package pmweb

import (
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

type etikettBody struct {
	Namn string `json:"namn"`
}

type planBody struct {
	Titel    string `json:"titel"`
	Innehall string `json:"innehall"`
}

// hamtaTaskdetaljer ger allt redigeringspanelen visar: taskens fält, dess
// etiketter, projektets övriga etiketter och planerna.
func (s *Server) hamtaTaskdetaljer(w http.ResponseWriter, r *http.Request) {
	tasks := nyTaskService(s)
	task, err := tasks.Get(r.Context(), r.PathValue("id"), true, false)
	if err != nil {
		svaraFel(w, errors.New("tasken finns inte"), http.StatusNotFound)
		return
	}
	valbara, err := service.NewLabelService(s.db).ListForProject(r.Context(), task.ProjectID)
	if err != nil {
		svaraFel(w, fmt.Errorf("kunde inte läsa projektets etiketter: %w", err), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{
		"ref": taskRef(task), "titel": task.Title, "beskrivning": task.Description,
		"typ": task.Type, "status": task.Status, "prioritet": task.Priority,
		"etiketter": task.Labels, "valbara_etiketter": valbara, "planer": task.Plans,
	})
}

// taBortTask raderar tasken. En task med en körning som väntar eller pågår
// får stå kvar, annars tappar körningen sitt underlag mitt i arbetet.
func (s *Server) taBortTask(w http.ResponseWriter, r *http.Request) {
	tasks := nyTaskService(s)
	task, err := tasks.Get(r.Context(), r.PathValue("id"), false, false)
	if err != nil {
		svaraFel(w, errors.New("tasken finns inte"), http.StatusNotFound)
		return
	}
	pagaende, err := s.pagaendeKorning(r, task.ID)
	if err != nil {
		svaraFel(w, err, http.StatusInternalServerError)
		return
	}
	if pagaende != "" {
		svaraFel(w, fmt.Errorf("tasken har en körning som %s. Avbryt den först", pagaende), http.StatusConflict)
		return
	}
	if err := tasks.Delete(r.Context(), r.PathValue("id"), s.aktor); err != nil {
		svaraFel(w, fmt.Errorf("kunde inte ta bort tasken: %w", err), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"ref": taskRef(task)})
}

// pagaendeKorning säger hur en väntande eller pågående körning ska beskrivas,
// och tom sträng när ingen finns.
func (s *Server) pagaendeKorning(r *http.Request, taskID string) (string, error) {
	var status string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT status FROM pm_korningar WHERE task_id=? AND status IN (?,?) ORDER BY skapad_at DESC LIMIT 1`,
		taskID, pm.StatusKoad, pm.StatusKor).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("kunde inte läsa taskens körningar: %w", err)
	}
	if status == pm.StatusKoad {
		return "väntar på tur", nil
	}
	return "pågår", nil
}

// laggEtikett sätter en etikett på tasken. Etiketten skapas om den är ny.
func (s *Server) laggEtikett(w http.ResponseWriter, r *http.Request) {
	var body etikettBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa etiketten"), http.StatusBadRequest)
		return
	}
	body.Namn = strings.TrimSpace(body.Namn)
	if body.Namn == "" {
		svaraFel(w, errors.New("ange etikettens namn"), http.StatusBadRequest)
		return
	}
	task, ok := s.taskForEtikett(w, r)
	if !ok {
		return
	}
	if err := service.NewLabelService(s.db).AttachByName(r.Context(), task.ProjectID, task.ID, body.Namn, s.aktor); err != nil {
		svaraFel(w, begripligtEtikettfel(err), http.StatusBadRequest)
		return
	}
	s.svaraMedEtiketter(w, r)
}

// taBortEtikett plockar bort etiketten från tasken. Etiketten själv blir kvar
// i projektet, så andra tasks behåller sin.
func (s *Server) taBortEtikett(w http.ResponseWriter, r *http.Request) {
	namn := strings.TrimSpace(r.PathValue("namn"))
	task, ok := s.taskForEtikett(w, r)
	if !ok {
		return
	}
	if err := service.NewLabelService(s.db).Detach(r.Context(), task.ID, task.ProjectID, namn, s.aktor); err != nil {
		svaraFel(w, begripligtEtikettfel(err), http.StatusBadRequest)
		return
	}
	s.svaraMedEtiketter(w, r)
}

// sparaPlan fäster en plan på tasken. Planen läses i redigeringspanelen.
func (s *Server) sparaPlan(w http.ResponseWriter, r *http.Request) {
	var body planBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa planen"), http.StatusBadRequest)
		return
	}
	body.Titel = strings.TrimSpace(body.Titel)
	body.Innehall = strings.TrimSpace(body.Innehall)
	if body.Titel == "" {
		body.Titel = "Plan"
	}
	if body.Innehall == "" {
		svaraFel(w, errors.New("skriv planens innehåll"), http.StatusBadRequest)
		return
	}
	tasks := nyTaskService(s)
	task, err := tasks.Get(r.Context(), r.PathValue("id"), false, false)
	if err != nil {
		svaraFel(w, errors.New("tasken finns inte"), http.StatusNotFound)
		return
	}
	plan, err := service.NewPlanService(s.db).Create(r.Context(), models.CreatePlanInput{
		TaskID: task.ID, Title: body.Titel, Body: body.Innehall, Actor: s.aktor,
	})
	if err != nil {
		svaraFel(w, fmt.Errorf("kunde inte spara planen: %w", err), http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusCreated, map[string]any{"plan": plan})
}

func (s *Server) taskForEtikett(w http.ResponseWriter, r *http.Request) (*models.Task, bool) {
	task, err := nyTaskService(s).Get(r.Context(), r.PathValue("id"), false, false)
	if err != nil {
		svaraFel(w, errors.New("tasken finns inte"), http.StatusNotFound)
		return nil, false
	}
	return task, true
}

// svaraMedEtiketter läser om tasken, så klienten får listan som den blev.
func (s *Server) svaraMedEtiketter(w http.ResponseWriter, r *http.Request) {
	task, err := nyTaskService(s).Get(r.Context(), r.PathValue("id"), false, false)
	if err != nil {
		svaraFel(w, errors.New("tasken finns inte"), http.StatusNotFound)
		return
	}
	valbara, err := service.NewLabelService(s.db).ListForProject(r.Context(), task.ProjectID)
	if err != nil {
		svaraFel(w, fmt.Errorf("kunde inte läsa projektets etiketter: %w", err), http.StatusInternalServerError)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{"etiketter": task.Labels, "valbara_etiketter": valbara})
}

// begripligtRedigeringsfel översätter felen som redigeringen kan ge. Den
// skiljer sig från begripligtTaskfel, som beskriver att lägga till en task.
func begripligtRedigeringsfel(err error) string {
	switch {
	case errors.Is(err, service.ErrTaskTitleRequired):
		return "ange taskens titel"
	case errors.Is(err, service.ErrTaskTitleTooLong):
		return "taskens titel får innehålla högst 255 tecken"
	case errors.Is(err, service.ErrTaskDescTooLong):
		return "beskrivningen får innehålla högst 65535 tecken"
	case errors.Is(err, service.ErrTaskTypeInvalid):
		return "välj en giltig typ"
	case errors.Is(err, service.ErrTaskStatusInvalid):
		return "välj en giltig status"
	case errors.Is(err, service.ErrTaskPriority):
		return "prioriteten måste vara P1-P5"
	default:
		return "PM kunde inte spara ändringen"
	}
}

func begripligtEtikettfel(err error) error {
	switch {
	case errors.Is(err, service.ErrLabelNameRequired):
		return errors.New("ange etikettens namn")
	case errors.Is(err, service.ErrLabelNotFound):
		return errors.New("tasken har ingen etikett med det namnet")
	default:
		return fmt.Errorf("kunde inte ändra etiketten: %w", err)
	}
}

func taskRef(task *models.Task) string { return fmt.Sprintf("TASK-%d", task.Seq) }
