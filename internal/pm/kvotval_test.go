package pm

import (
	"strings"
	"testing"
	"time"

	"github.com/mazen160/backlog/internal/timeutil"
)

func kvotlage(fem, sju float64, framat bool) Anvandning {
	nollstalls := timeutil.Now() + int64(time.Hour)
	if !framat {
		nollstalls = timeutil.Now() - int64(time.Hour)
	}
	return Anvandning{
		FemTimmar: &Kvotfonster{Andel: fem, NollstallsAt: nollstalls},
		SjuDagar:  &Kvotfonster{Andel: sju, NollstallsAt: nollstalls},
		AvlastAt:  timeutil.Now(),
	}
}

// gammaltKvotlage är samma läge avläst för så länge sedan att det inte längre
// får styra ett byte.
func gammaltKvotlage(fem, sju float64, alder time.Duration) Anvandning {
	lage := kvotlage(fem, sju, true)
	lage.AvlastAt = timeutil.Now() - int64(alder)
	return lage
}

func kvotkonfig() Konfig {
	return Konfig{
		Agenter: map[string]AgentKonfig{"claude": {}, "codex": {}, "trasig": {}},
		Kvot:    KvotKonfig{Tak: 0.9, Turordning: []string{"claude", "codex"}},
	}
}

// Det hårdast belastade fönstret styr. En agent med gott om femtimmarskvot men
// slut veckokvot kan inte ta arbetet.
func TestAnstrangningTarDetHardastBelastadeFonstret(t *testing.T) {
	hogst, kant := kvotlage(0.10, 0.95, true).Anstrangning()
	if !kant || hogst != 0.95 {
		t.Fatalf("veckofönstret skulle väga tyngst, fick %v (%v)", hogst, kant)
	}
	// Ett fönster som redan nollställts säger ingenting om läget nu.
	if _, kant := kvotlage(0.99, 0.99, false).Anstrangning(); kant {
		t.Fatal("passerade fönster ska inte räknas")
	}
	if _, kant := (Anvandning{Saknas: true}).Anstrangning(); kant {
		t.Fatal("en agent utan kvotläge har ingen ansträngning")
	}
}

func TestKvotenByterTillAgentenMedMestUtrymme(t *testing.T) {
	val := AgentVal{Agent: "claude", Motivering: "ingen regel matchade", Modell: "opus"}
	ny := ValjAgentEfterKvot(kvotkonfig(), val, false, map[string]Anvandning{
		"claude": kvotlage(0.95, 0.40, true),
		"codex":  kvotlage(0.14, 0.20, true),
	})
	if ny.Agent != "codex" {
		t.Fatalf("bytet uteblev: %+v", ny)
	}
	if !strings.Contains(ny.Motivering, "95") || !strings.Contains(ny.Motivering, "codex") {
		t.Fatalf("motiveringen förklarar inte bytet: %q", ny.Motivering)
	}
	// Modellen hörde till den gamla agenten och följer inte med.
	if ny.Modell != "" {
		t.Fatalf("modellen skulle nollställas vid byte, fick %q", ny.Modell)
	}
}

// Säger användaren --agent gäller det, oavsett kvot. PM ska inte köra något
// annat än det som står i utdelningen.
func TestOverstyrningVinnerOverKvoten(t *testing.T) {
	val := AgentVal{Agent: "claude", Motivering: "överstyrd med --agent claude"}
	ny := ValjAgentEfterKvot(kvotkonfig(), val, true, map[string]Anvandning{
		"claude": kvotlage(0.99, 0.99, true),
		"codex":  kvotlage(0.01, 0.01, true),
	})
	if ny.Agent != "claude" {
		t.Fatalf("överstyrningen kördes över: %+v", ny)
	}
}

func TestKvotenByterInteUtanSkal(t *testing.T) {
	fall := []struct {
		namn  string
		k     Konfig
		lagen map[string]Anvandning
	}{
		{"under taket", kvotkonfig(), map[string]Anvandning{
			"claude": kvotlage(0.50, 0.50, true), "codex": kvotlage(0.01, 0.01, true)}},
		{"alla fulla", kvotkonfig(), map[string]Anvandning{
			"claude": kvotlage(0.95, 0.95, true), "codex": kvotlage(0.97, 0.97, true)}},
		{"okänd kvot hos den andra", kvotkonfig(), map[string]Anvandning{
			"claude": kvotlage(0.95, 0.95, true)}},
		{"turordning saknas", Konfig{Agenter: map[string]AgentKonfig{"claude": {}, "codex": {}}},
			map[string]Anvandning{"claude": kvotlage(0.99, 0.99, true), "codex": kvotlage(0.01, 0.01, true)}},
	}
	for _, f := range fall {
		val := AgentVal{Agent: "claude", Motivering: "ingen regel matchade"}
		ny := ValjAgentEfterKvot(f.k, val, false, f.lagen)
		if ny.Agent != "claude" {
			t.Errorf("%s: agenten byttes utan skäl, blev %s", f.namn, ny.Agent)
		}
	}
}

// Står agenten inte i turordningen är den inte utbytbar, och en agent utanför
// turordningen får aldrig ta över.
func TestBaraAgenterITurordningenByts(t *testing.T) {
	k := kvotkonfig()
	val := AgentVal{Agent: "trasig", Motivering: "regeln matchade"}
	if ny := ValjAgentEfterKvot(k, val, false, map[string]Anvandning{
		"trasig": kvotlage(0.99, 0.99, true), "codex": kvotlage(0.01, 0.01, true),
	}); ny.Agent != "trasig" {
		t.Fatalf("en agent utanför turordningen ska lämnas i fred, blev %s", ny.Agent)
	}

	k.Kvot.Turordning = []string{"claude", "saknas-i-konfig"}
	val = AgentVal{Agent: "claude", Motivering: "regeln matchade"}
	if ny := ValjAgentEfterKvot(k, val, false, map[string]Anvandning{
		"claude": kvotlage(0.99, 0.99, true), "saknas-i-konfig": kvotlage(0.01, 0.01, true),
	}); ny.Agent != "claude" {
		t.Fatalf("en agent utan konfiguration får inte väljas, blev %s", ny.Agent)
	}
}

// Taket ska gå att sätta, och ett orimligt tak faller tillbaka på standarden.
func TestKvottaketGarAttSatta(t *testing.T) {
	k := kvotkonfig()
	k.Kvot.Tak = 0.5
	val := AgentVal{Agent: "claude", Motivering: "regeln matchade"}
	if ny := ValjAgentEfterKvot(k, val, false, map[string]Anvandning{
		"claude": kvotlage(0.60, 0.10, true), "codex": kvotlage(0.10, 0.10, true),
	}); ny.Agent != "codex" {
		t.Fatalf("taket 0,5 skulle ge byte vid 60 procent, blev %s", ny.Agent)
	}

	k.Kvot.Tak = 4
	if ny := ValjAgentEfterKvot(k, val, false, map[string]Anvandning{
		"claude": kvotlage(0.95, 0.10, true), "codex": kvotlage(0.10, 0.10, true),
	}); ny.Agent != "codex" {
		t.Fatalf("ett orimligt tak ska falla tillbaka på %v, blev %s", StandardKvottak, ny.Agent)
	}
}

// Förbrukningen växer mellan avläsningarna, så en gammal siffra är alltid för
// låg. Ett läge från i förrgår får varken flytta arbete eller ta emot det.
func TestGammalAvlasningStyrInteValet(t *testing.T) {
	val := AgentVal{Agent: "claude", Motivering: "ingen regel matchade"}

	// Den valda agenten ser full ut, men siffran är två dygn gammal.
	ny := ValjAgentEfterKvot(kvotkonfig(), val, false, map[string]Anvandning{
		"claude": gammaltKvotlage(0.99, 0.99, 48*time.Hour),
		"codex":  kvotlage(0.10, 0.10, true),
	})
	if ny.Agent != "claude" {
		t.Fatalf("en gammal avläsning flyttade arbetet: %+v", ny)
	}

	// Den andra agenten ser ledig ut, men siffran är lika gammal.
	ny = ValjAgentEfterKvot(kvotkonfig(), val, false, map[string]Anvandning{
		"claude": kvotlage(0.95, 0.95, true),
		"codex":  gammaltKvotlage(0.10, 0.10, 48*time.Hour),
	})
	if ny.Agent != "claude" {
		t.Fatalf("arbetet gick till en agent med gammal avläsning: %+v", ny)
	}
	if !strings.Contains(ny.Motivering, "ingen annan agent har utrymme") {
		t.Fatalf("motiveringen säger inte varför bytet uteblev: %q", ny.Motivering)
	}
}

// Ett veckofönster nollställs var sjunde dag. En avläsning som gjordes före
// det pågående fönstret började gäller ett fönster som redan är slut.
func TestFonsterFranEnTidigarePeriodRaknasInte(t *testing.T) {
	nu := timeutil.Now()
	lage := Anvandning{
		// Fönstret nollställs om en timme, alltså började det för sju dagar
		// sedan minus en timme. Avläsningen gjordes före det.
		SjuDagar: &Kvotfonster{Andel: 0.99, NollstallsAt: nu + int64(time.Hour)},
		AvlastAt: nu - int64(8*24*time.Hour),
	}
	if _, kant := lage.Anstrangning(); kant {
		t.Fatal("en avläsning från ett tidigare fönster ska inte räknas")
	}

	// Samma fönster, men avläst inne i perioden.
	lage.AvlastAt = nu - int64(time.Hour)
	hogst, kant := lage.Anstrangning()
	if !kant || hogst != 0.99 {
		t.Fatalf("avläsningen inne i fönstret skulle räknas, fick %v (%v)", hogst, kant)
	}
}
