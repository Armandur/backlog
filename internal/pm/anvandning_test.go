package pm

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTolkaClaudeKvotlageFranInspeladRad(t *testing.T) {
	fil, err := os.Open(filepath.Join("testdata", "claude-rate-limit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer fil.Close()

	skanner := bufio.NewScanner(fil)
	if !skanner.Scan() {
		t.Fatalf("testfilen saknar rad: %v", skanner.Err())
	}
	// Raden är brus i förloppsvyn, men kvotläget ska gå att läsa ur strömmen.
	if handelser := tolkaClaudeRad(skanner.Bytes()); len(handelser) != 0 {
		t.Fatalf("kvotraden ska inte bli en händelse: %+v", handelser)
	}
	lage := AnvandningUrClaudeStrom(string(skanner.Bytes()))
	if lage == nil {
		t.Fatal("fick inget kvotläge ur strömmen")
	}
	if lage.Status != "allowed" || lage.Kvottyp != "five_hour" {
		t.Fatalf("fel status eller kvottyp: %+v", lage)
	}
	if lage.FemTimmar.Andel != 0.44 || lage.FemTimmar.NollstallsAt != 1789141800*int64(time.Second) {
		t.Fatalf("fel femtimmarsfönster: %+v", lage.FemTimmar)
	}
	if lage.SjuDagar.Andel != 0.63 || lage.SjuDagar.NollstallsAt != 1789480800*int64(time.Second) {
		t.Fatalf("fel sjudagarsfönster: %+v", lage.SjuDagar)
	}
	if lage.AvlastAt == 0 {
		t.Fatal("kvotläget saknar avläsningstid")
	}
}

func TestAnvandningUrClaudeStromTarDenSistaRaden(t *testing.T) {
	tidig := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":0.10,"resetsAt":1},"seven_day":{"utilization":0.20,"resetsAt":2}}}}`
	sen := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":0.80,"resetsAt":3},"seven_day":{"utilization":0.90,"resetsAt":4}}}}`

	lage := AnvandningUrClaudeStrom(tidig + "\n" + `{"type":"assistant"}` + "\n" + sen)
	if lage == nil || lage.FemTimmar.Andel != 0.80 {
		t.Fatalf("den sista raden gällde inte: %+v", lage)
	}
	if AnvandningUrClaudeStrom(`{"type":"assistant"}`) != nil {
		t.Fatal("en ström utan kvotläge gav ett läge")
	}
}

func TestAnvandningSparasPerAgentOchOverlevar(t *testing.T) {
	db := testDB(t)
	store := NewAnvandningStore(db)
	lage := Anvandning{
		Agent: "claude", Status: "allowed", Kvottyp: "five_hour",
		FemTimmar: &Kvotfonster{Andel: 0.44, NollstallsAt: 1789141800},
		SjuDagar:  &Kvotfonster{Andel: 0.63, NollstallsAt: 1789480800},
		AvlastAt:  1789232000,
	}
	if err := store.Spara(context.Background(), lage); err != nil {
		t.Fatal(err)
	}

	// En ny anslutning mot samma fil visar att läget ligger i databasen.
	last, err := NewAnvandningStore(db).Hamta(context.Background(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if last.FemTimmar == nil || last.FemTimmar.Andel != 0.44 || last.AvlastAt != 1789232000 {
		t.Fatalf("kvotläget kom tillbaka fel: %+v", last)
	}

	// Ett nyare läge ersätter det gamla, en rad per agent.
	lage.FemTimmar = &Kvotfonster{Andel: 0.71, NollstallsAt: 1789141800}
	lage.AvlastAt = 1789233000
	if err := store.Spara(context.Background(), lage); err != nil {
		t.Fatal(err)
	}
	last, err = store.Hamta(context.Background(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	if last.FemTimmar.Andel != 0.71 {
		t.Fatalf("det nyare läget gällde inte: %+v", last)
	}
	var rader int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_anvandning`).Scan(&rader); err != nil {
		t.Fatal(err)
	}
	if rader != 1 {
		t.Fatalf("väntade en rad per agent, fick %d", rader)
	}
}
