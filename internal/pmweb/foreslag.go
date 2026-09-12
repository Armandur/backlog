package pmweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/pm"
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

type foreslaNyTaskBody struct {
	Text  string `json:"text"`
	Agent string `json:"agent"`
}

type klassningsforslag struct {
	Typ          models.TaskType `json:"typ"`
	Prioritet    int             `json:"prioritet"`
	Modell       string          `json:"modell"`
	Anstrangning string          `json:"anstrangning"`
}

type klassningsvarden struct {
	Modeller       []string
	Anstrangningar []string
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

type nyttTaskForslag struct {
	Titel       string          `json:"titel"`
	Beskrivning string          `json:"beskrivning"`
	Typ         models.TaskType `json:"typ"`
	Prioritet   int             `json:"prioritet"`
}

type uppdateraTaskBody struct {
	Titel       *string            `json:"titel"`
	Beskrivning *string            `json:"beskrivning"`
	Typ         *models.TaskType   `json:"typ"`
	Prioritet   *int               `json:"prioritet"`
	Status      *models.TaskStatus `json:"status"`
}

func (s *Server) foreslaNyTask(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")
	if _, err := service.NewProjectService(s.db).GetByAlias(r.Context(), alias); err != nil {
		svaraFel(w, fmt.Errorf("projektet %q finns inte i PM-workspacet", alias), http.StatusNotFound)
		return
	}

	var body foreslaNyTaskBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		svaraFel(w, errors.New("kunde inte läsa texten som ska bli en task"), http.StatusBadRequest)
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		svaraFel(w, errors.New("ange texten som ska bli en task"), http.StatusBadRequest)
		return
	}
	if len([]rune(body.Text)) < 10 {
		svaraFel(w, errors.New("texten måste innehålla minst tio tecken"), http.StatusBadRequest)
		return
	}

	agent, err := s.aktuelltRegister().Hamta(strings.TrimSpace(body.Agent))
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}
	ctx, avbryt := context.WithTimeout(r.Context(), taskForslagTimeout)
	defer avbryt()
	svar, err := agent.Fraga(ctx, byggNyttTaskForslagsprompt(body.Text))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			svaraFel(w, errors.New("agenten hann inte skapa ett förslag"), http.StatusGatewayTimeout)
			return
		}
		svaraFel(w, fmt.Errorf("agenten kunde inte skapa förslaget: %w", err), http.StatusBadGateway)
		return
	}

	var forslag nyttTaskForslag
	if err := tolkaNyttTaskForslag(strings.TrimSpace(svar), &forslag); err != nil {
		meddelande, kod := begripligtTaskfel(err)
		if errors.Is(err, service.ErrTaskDescRequired) {
			meddelande = "agentens förslag saknar en beskrivning"
		} else if strings.Contains(err.Error(), "giltig JSON") {
			meddelande = err.Error()
		} else if kod == http.StatusInternalServerError {
			meddelande = err.Error()
		}
		svaraFel(w, errors.New(meddelande), http.StatusBadGateway)
		return
	}
	svaraJSON(w, http.StatusOK, forslag)
}

func tolkaNyttTaskForslag(svar string, forslag *nyttTaskForslag) error {
	if err := json.Unmarshal([]byte(svar), forslag); err != nil {
		return errors.New("agenten svarade inte med giltig JSON")
	}
	forslag.Titel = kortaTitel(strings.TrimSpace(forslag.Titel), 255)
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
	if err := service.ValidateTaskType(forslag.Typ); err != nil {
		return err
	}
	return service.ValidateTaskPriority(forslag.Prioritet)
}

func byggNyttTaskForslagsprompt(text string) string {
	typer := make([]string, 0, len(models.AllTaskTypes()))
	for _, taskTyp := range models.AllTaskTypes() {
		typer = append(typer, string(taskTyp))
	}
	return fmt.Sprintf(`Gör om texten till en komplett task på svenska.
Svara endast med ett JSON-objekt utan kodstaket eller förklaringar.
Objektet ska ha fälten titel, beskrivning, typ och prioritet.
Titeln ska vara kort. Beskrivningen ska ha rubrikerna Kontext, Acceptanskriterier och Verifiering.
Typ måste vara ett av följande värden: %s.
Prioritet måste vara ett heltal från 1 till 5.

Text:
%s`, strings.Join(typer, ", "), text)
}

// kortaTitel klipper på tecken, samma enhet som tasktjänsten mäter i. Klipper
// vi på byte kapas en svensk titel vid halva den tillåtna längden.
func kortaTitel(titel string, maxTecken int) string {
	tecken := []rune(titel)
	if len(tecken) <= maxTecken {
		return titel
	}
	return string(tecken[:maxTecken])
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
	var varden klassningsvarden
	if body.Sort == forslagKlassning {
		varden, err = s.hamtaKlassningsvarden(r.Context())
		if err != nil {
			svaraFel(w, err, http.StatusInternalServerError)
			return
		}
	}

	ctx, avbryt := context.WithTimeout(r.Context(), taskForslagTimeout)
	defer avbryt()
	svar, err := agent.Fraga(ctx, byggTaskForslagsprompt(body.Sort, task, varden))
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
	if err := tolkaTaskForslag(strings.TrimSpace(svar), &utkast, varden); err != nil {
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

func tolkaTaskForslag(svar string, utkast *taskUtkast, varden klassningsvarden) error {
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
		forslag.Modell = strings.TrimSpace(forslag.Modell)
		forslag.Anstrangning = strings.TrimSpace(forslag.Anstrangning)
		if err := valideraKlassningsvarde("modellvärde", forslag.Modell, varden.Modeller); err != nil {
			return err
		}
		if err := valideraKlassningsvarde("ansträngningsvärde", forslag.Anstrangning, varden.Anstrangningar); err != nil {
			return err
		}
		utkast.Typ = forslag.Typ
		utkast.Prioritet = forslag.Prioritet
		utkast.Modell = forslag.Modell
		utkast.Anstrangning = forslag.Anstrangning
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

func byggTaskForslagsprompt(sort string, task *models.Task, varden klassningsvarden) string {
	if sort == forslagKlassning {
		typer := make([]string, 0, len(models.AllTaskTypes()))
		for _, taskTyp := range models.AllTaskTypes() {
			typer = append(typer, string(taskTyp))
		}
		falt := "typ, prioritet och modell"
		modellregel := "Inga kända modeller finns. Lämna modell som en tom sträng."
		if len(varden.Modeller) > 0 {
			modellregel = fmt.Sprintf("Modell måste vara ett av följande värden eller en tom sträng: %s.", strings.Join(varden.Modeller, ", "))
		}
		anstrangningsregel := ""
		if len(varden.Anstrangningar) > 0 {
			falt += " och anstrangning"
			anstrangningsregel = fmt.Sprintf("\nAnstrangning måste vara ett av följande värden eller en tom sträng: %s.", strings.Join(varden.Anstrangningar, ", "))
		}
		return fmt.Sprintf(`Klassificera tasken utifrån titeln och beskrivningen.
Svara endast med ett JSON-objekt utan kodstaket eller förklaringar.
Objektet ska ha fälten %s.
Typ måste vara ett av följande värden: %s.
Prioritet måste vara ett heltal från 1 till 5.
%s%s

Titel: %s
Beskrivning:
%s`, falt, strings.Join(typer, ", "), modellregel, anstrangningsregel, task.Title, task.Description)
	}
	return fmt.Sprintf(`Berika tasken utifrån den nuvarande titeln och beskrivningen.
Svara endast med ett JSON-objekt utan kodstaket eller förklaringar.
Objektet ska ha fälten titel och beskrivning.
Skriv på svenska. Beskrivningen ska ha rubrikerna Kontext, Acceptanskriterier och Verifiering.

Nuvarande titel: %s
Nuvarande beskrivning:
%s`, task.Title, task.Description)
}

func (s *Server) hamtaKlassningsvarden(ctx context.Context) (klassningsvarden, error) {
	konfig, err := pm.LasKonfig(konfigWorkDir())
	if err != nil {
		return klassningsvarden{}, err
	}
	modeller := map[string]struct{}{}
	anstrangningar := map[string]struct{}{}
	for _, agent := range konfig.Agenter {
		laggTillKlassningsvarde(modeller, agent.Modell)
		laggTillKlassningsvarde(anstrangningar, agent.Anstrangning)
	}
	// Bara de senaste körningarna behövs. Utan gräns läses hela tabellen vid
	// varje klassning, och den växer.
	korningar, err := pm.NewKorningStore(s.db).Lista(ctx, "", 200)
	if err != nil {
		return klassningsvarden{}, err
	}
	for _, korning := range korningar {
		laggTillKlassningsvarde(modeller, korning.Modell)
		if len(anstrangningar) > 0 {
			laggTillKlassningsvarde(anstrangningar, korning.Anstrangning)
		}
	}
	return klassningsvarden{
		Modeller:       nycklar(modeller),
		Anstrangningar: nycklar(anstrangningar),
	}, nil
}

func laggTillKlassningsvarde(varden map[string]struct{}, varde string) {
	if varde = strings.TrimSpace(varde); varde != "" {
		varden[varde] = struct{}{}
	}
}

func nycklar(varden map[string]struct{}) []string {
	resultat := make([]string, 0, len(varden))
	for varde := range varden {
		resultat = append(resultat, varde)
	}
	sort.Strings(resultat)
	return resultat
}

func valideraKlassningsvarde(namn, varde string, tillatna []string) error {
	if varde == "" {
		return nil
	}
	for _, tillatet := range tillatna {
		if varde == tillatet {
			return nil
		}
	}
	return fmt.Errorf("agentens förslag innehåller ett okänt %s %q", namn, varde)
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
	// Statusen byter väg genom Move, så den flyttas först. Faller fältbytet
	// efteråt säger svaret vilken halva som landade.
	if body.Status != nil {
		if _, err := tasks.Move(r.Context(), r.PathValue("id"), *body.Status, s.aktor); err != nil {
			svaraFel(w, errors.New(begripligtRedigeringsfel(err)), http.StatusBadRequest)
			return
		}
	}
	task, err := tasks.Update(r.Context(), r.PathValue("id"), models.UpdateTaskInput{
		Title: body.Titel, Description: body.Beskrivning, Type: body.Typ, Priority: body.Prioritet,
	}, s.aktor)
	if err != nil {
		meddelande := begripligtRedigeringsfel(err)
		if body.Status != nil {
			meddelande = "statusen är ändrad, men resten sparades inte: " + meddelande
		}
		svaraFel(w, errors.New(meddelande), http.StatusBadRequest)
		return
	}
	svaraJSON(w, http.StatusOK, map[string]any{
		"ref": fmt.Sprintf("TASK-%d", task.Seq), "titel": task.Title,
		"beskrivning": task.Description, "typ": task.Type,
		"prioritet": task.Priority, "status": task.Status,
	})
}

func nyTaskService(s *Server) *service.TaskService {
	return service.NewTaskService(s.db, service.NewPlanService(s.db), service.NewLabelService(s.db))
}
