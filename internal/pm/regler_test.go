package pm

import (
	"strings"
	"testing"
)

func testKonfig() Konfig {
	k := StandardKonfig()
	k.Regler = []Regel{
		{Namn: "buggar till codex", Typ: []string{"bug"}, Agent: "codex"},
		{Namn: "granskning till codex", Etiketter: []string{"granskning"}, Agent: "codex"},
		{Namn: "webb till claude", Nyckelord: []string{"webb-ui", "tråd-vy"}, Agent: "claude"},
		{Namn: "typ och etikett", Typ: []string{"chore"}, Etiketter: []string{"infra"}, Agent: "codex"},
	}
	return k
}

func TestValjAgentPaTyp(t *testing.T) {
	val, err := ValjAgent(testKonfig(), TaskFakta{Typ: "bug", Titel: "Trasig knapp"}, "")
	if err != nil || val.Agent != "codex" {
		t.Fatalf("bug skulle gå till codex, fick %+v %v", val, err)
	}
	if !strings.Contains(val.Motivering, "buggar till codex") || !strings.Contains(val.Motivering, "typ=bug") {
		t.Fatalf("motiveringen ska namnge regeln och skälet, fick %q", val.Motivering)
	}
}

func TestValjAgentPaEtikettOchNyckelord(t *testing.T) {
	val, _ := ValjAgent(testKonfig(), TaskFakta{Typ: "task", Etiketter: []string{"granskning"}}, "")
	if val.Agent != "codex" {
		t.Fatalf("etikett granskning skulle ge codex, fick %q", val.Agent)
	}
	val, _ = ValjAgent(testKonfig(), TaskFakta{Typ: "task", Titel: "Bygg Tråd-vyn"}, "")
	if val.Agent != "claude" || !strings.Contains(val.Motivering, "nyckelord=tråd-vy") {
		t.Fatalf("nyckelord skulle matcha oavsett versaler, fick %+v", val)
	}
}

// En regel med flera villkor kräver att alla stämmer.
func TestValjAgentKraverAllaVillkor(t *testing.T) {
	k := Konfig{
		DefaultAgent: "claude",
		Agenter:      StandardKonfig().Agenter,
		Regler:       []Regel{{Namn: "bara båda", Typ: []string{"chore"}, Etiketter: []string{"infra"}, Agent: "codex"}},
	}
	val, _ := ValjAgent(k, TaskFakta{Typ: "chore", Etiketter: []string{"annat"}}, "")
	if val.Agent != "claude" {
		t.Fatalf("halv träff ska falla till default, fick %q", val.Agent)
	}
	val, _ = ValjAgent(k, TaskFakta{Typ: "chore", Etiketter: []string{"infra"}}, "")
	if val.Agent != "codex" {
		t.Fatalf("full träff ska ge codex, fick %q", val.Agent)
	}
}

func TestValjAgentDefaultUtanTraff(t *testing.T) {
	val, err := ValjAgent(testKonfig(), TaskFakta{Typ: "task", Titel: "Något helt annat"}, "")
	if err != nil || val.Agent != "claude" {
		t.Fatalf("utan träff ska default-agenten väljas, fick %+v %v", val, err)
	}
	if !strings.Contains(val.Motivering, "ingen regel matchade") {
		t.Fatalf("motiveringen ska säga att ingen regel matchade, fick %q", val.Motivering)
	}
}

func TestValjAgentOverstyrning(t *testing.T) {
	val, err := ValjAgent(testKonfig(), TaskFakta{Typ: "bug"}, "claude")
	if err != nil || val.Agent != "claude" || !strings.Contains(val.Motivering, "--agent claude") {
		t.Fatalf("överstyrningen ska vinna över regeln, fick %+v %v", val, err)
	}
	if _, err := ValjAgent(testKonfig(), TaskFakta{}, "finns-inte"); err == nil {
		t.Fatal("okänd agent i --agent ska ge fel")
	}
}

// Regeln får inte matcha en förstaordsträff i ett annat ord av misstag.
func TestForstaMatchandeRegelnVinner(t *testing.T) {
	k := Konfig{
		DefaultAgent: "claude",
		Agenter:      StandardKonfig().Agenter,
		Regler: []Regel{
			{Namn: "först", Typ: []string{"bug"}, Agent: "claude"},
			{Namn: "sedan", Typ: []string{"bug"}, Agent: "codex"},
		},
	}
	val, _ := ValjAgent(k, TaskFakta{Typ: "bug"}, "")
	if val.Agent != "claude" || !strings.Contains(val.Motivering, "först") {
		t.Fatalf("första regeln ska vinna, fick %+v", val)
	}
}
