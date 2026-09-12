package pm

import (
	"context"
	"strings"
	"testing"

	"github.com/mazen160/backlog/internal/models"
)

func TestDelaAgentsvarPlockarBortMinnessektionen(t *testing.T) {
	text, minne := delaAgentsvar("Svar till användaren.\n\n<pm-minne>\nEtt bestående beslut.\n</pm-minne>")
	if text != "Svar till användaren." || minne != "Ett bestående beslut." {
		t.Fatalf("oväntad delning: %q och %q", text, minne)
	}

	orort := "Text med <pm-minne> mitt i svaret."
	text, minne = delaAgentsvar(orort)
	if text != orort || minne != "" {
		t.Fatalf("PM ändrade ett vanligt svar: %q och %q", text, minne)
	}
}

func TestPromptAvgransarProjektminnet(t *testing.T) {
	prompt := ByggPrompt("Projekt: demo", "Vad valde vi?")
	for _, text := range []string{
		"beslut, vägval eller fakta som gäller framåt",
		"dagsläge eller svar på en engångsfråga",
		"<pm-minne>förslag</pm-minne>",
	} {
		if !strings.Contains(prompt, text) {
			t.Fatalf("prompten saknar %q:\n%s", text, prompt)
		}
	}
}

func TestFragaSpararMinnesforslagSeparat(t *testing.T) {
	db := testDB(t)
	pid := testProjekt(t, db, "demo")
	agent := &fakeAgent{
		namn: "fake-modell",
		svar: "Vi valde SQLite.\n<pm-minne>Projektet använder SQLite.</pm-minne>",
	}
	reg := NewAgentRegister()
	reg.Registrera(agent)

	post, err := Fraga(context.Background(), db, reg, FragaInput{
		Alias: "demo", ProjectID: pid, Fraga: "Vad valde vi?",
		Fragare: models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if post.Text != "Vi valde SQLite." || post.Minnesforslag != "Projektet använder SQLite." {
		t.Fatalf("PM sparade svaret fel: %+v", post)
	}
}
