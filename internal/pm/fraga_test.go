package pm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/timeutil"
)

// fakeAgent håller go test deterministiskt: ingen live-LLM i sviten.
type fakeAgent struct {
	namn  string
	sedd  string
	svar  string
	felet error
	anrop int
}

func (f *fakeAgent) Namn() string { return f.namn }

func (f *fakeAgent) Fraga(ctx context.Context, prompt string) (string, error) {
	f.anrop++
	f.sedd = prompt
	if f.felet != nil {
		return "", f.felet
	}
	return f.svar, nil
}

func (f *fakeAgent) Kor(ctx context.Context, in KorInput) (Resultat, error) {
	f.anrop++
	f.sedd = in.Brief
	if in.VidHandelse != nil {
		in.VidHandelse(Handelse{Tid: timeutil.Now(), Sort: "verktyg", Text: "läser projektet"})
	}
	if f.felet != nil {
		return Resultat{ExitKod: 1}, f.felet
	}
	return Resultat{Utdata: f.svar}, nil
}

func TestFragaSparerFragaOchSvarMedKontext(t *testing.T) {
	db := testDB(t)
	pid := testProjekt(t, db, "demo")
	nu := timeutil.Now()
	if _, err := db.Exec(`INSERT INTO tasks(id, project_id, title, status, priority, task_seq, created_at, updated_at)
	                      VALUES(?,?,?,?,?,?,?,?)`,
		ids.New(), pid, "Bygg tråd-vyn", "todo", 2, 4711, nu, nu); err != nil {
		t.Fatalf("skapa task: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO project_memory(id, project_id, body, tags, actor_kind, actor_name, created_at)
	                      VALUES(?,?,?,?,?,?,?)`,
		ids.New(), pid, "PM-databasen är skild från vardagsdatabasen.", "status", "human", "rasmus", nu); err != nil {
		t.Fatalf("skapa minne: %v", err)
	}

	agent := &fakeAgent{namn: "fake-modell", svar: "TASK-4711 är öppen."}
	reg := NewAgentRegister()
	reg.Registrera(agent)

	svar, err := Fraga(context.Background(), db, reg, FragaInput{
		Alias: "demo", ProjectID: pid, Fraga: "vilka tasks är öppna?",
		Fragare: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
	})
	if err != nil {
		t.Fatalf("fraga: %v", err)
	}
	if svar.Actor.Kind != models.ActorKindAI || svar.Actor.Name != "fake-modell" {
		t.Fatalf("svaret ska sparas som ai:<agent>, fick %+v", svar.Actor)
	}

	for _, vantat := range []string{"TASK-4711", "Bygg tråd-vyn", "PM-databasen är skild", "vilka tasks är öppna?", "Projekt: demo"} {
		if !strings.Contains(agent.sedd, vantat) {
			t.Fatalf("prompten saknar %q:\n%s", vantat, agent.sedd)
		}
	}

	poster, err := NewSamtalStore(db).List(context.Background(), pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(poster) != 2 || poster[0].Text != "vilka tasks är öppna?" || poster[1].Text != "TASK-4711 är öppen." {
		t.Fatalf("tråden ska innehålla fråga och svar, fick %+v", poster)
	}
}

func TestFragaGerFelUtanAgent(t *testing.T) {
	db := testDB(t)
	pid := testProjekt(t, db, "demo")
	reg := NewAgentRegister()
	_, err := Fraga(context.Background(), db, reg, FragaInput{
		Alias: "demo", ProjectID: pid, Fraga: "hej",
		Fragare: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
	})
	if err == nil {
		t.Fatal("fråga utan registrerad agent ska ge fel")
	}
}

func TestFragaValjerNamngivenAgent(t *testing.T) {
	db := testDB(t)
	pid := testProjekt(t, db, "demo")
	forst := &fakeAgent{namn: "forst", svar: "a"}
	andra := &fakeAgent{namn: "andra", svar: "b"}
	reg := NewAgentRegister()
	reg.Registrera(forst)
	reg.Registrera(andra)

	svar, err := Fraga(context.Background(), db, reg, FragaInput{
		Alias: "demo", ProjectID: pid, Fraga: "hej", Agent: "andra",
		Fragare: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if svar.Actor.Name != "andra" || andra.anrop != 1 || forst.anrop != 0 {
		t.Fatalf("fel agent svarade: %+v", svar.Actor)
	}
	if _, err := reg.Hamta("finns-inte"); err == nil {
		t.Fatal("okänd agent gav inget fel")
	}
}

func TestFragaSparerIngetSvarNarAgentenFelar(t *testing.T) {
	db := testDB(t)
	pid := testProjekt(t, db, "demo")
	reg := NewAgentRegister()
	reg.Registrera(&fakeAgent{namn: "trasig", felet: errors.New("nere")})

	if _, err := Fraga(context.Background(), db, reg, FragaInput{
		Alias: "demo", ProjectID: pid, Fraga: "hej",
		Fragare: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
	}); err == nil {
		t.Fatal("väntade fel från agenten")
	}
	poster, err := NewSamtalStore(db).List(context.Background(), pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(poster) != 1 {
		t.Fatalf("bara frågan ska ligga kvar i tråden, fick %d inlägg", len(poster))
	}
}

// Adaptern får inte anta Claude: registret ska kunna köras utan den.
func TestRegisterArAgentoberoende(t *testing.T) {
	reg := NewAgentRegister()
	reg.Registrera(&fakeAgent{namn: "lokal-modell", svar: "hej"})
	a, err := reg.Hamta("")
	if err != nil || a.Namn() != "lokal-modell" {
		t.Fatalf("förvalet ska vara den enda registrerade agenten, fick %v %v", a, err)
	}
}
