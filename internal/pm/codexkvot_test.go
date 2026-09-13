package pm

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// medCodexKorare byter ut app-servern mot en attrapp. Inget prov får starta
// riktiga codex, för då beror utfallet på maskinens inloggning.
func medCodexKorare(t *testing.T, korare codexKorare) {
	t.Helper()
	gammal := korCodexAppServer
	korCodexAppServer = korare
	t.Cleanup(func() { korCodexAppServer = gammal })
}

// Svaret är inspelat från en riktig app-server, se
// testdata/codex-appserver-kvot.jsonl. Kvoten ligger på den fjärde raden,
// efter två notifikationer som PM måste hoppa över.
func TestCodexKvotLaserVeckofonstretUrAppServern(t *testing.T) {
	inspelat, err := os.ReadFile("testdata/codex-appserver-kvot.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var sedd string
	medCodexKorare(t, func(_ context.Context, kommando, indata string) ([]byte, error) {
		sedd = indata
		if kommando != "codex" {
			t.Errorf("fel kommando: %q", kommando)
		}
		return inspelat, nil
	})

	lage := CodexKvot(context.Background(), "codex")
	if lage == nil {
		t.Fatal("kvoten lästes inte ur svaret")
	}
	if lage.SjuDagar == nil || lage.SjuDagar.Andel != 0.14 {
		t.Fatalf("sjudagarsfönstret kom fel: %+v", lage.SjuDagar)
	}
	// Codex räknar i sekunder, PM i nanosekunder.
	if lage.SjuDagar.NollstallsAt != 1789829030*int64(time.Second) {
		t.Fatalf("nollställningen räknades inte om: %d", lage.SjuDagar.NollstallsAt)
	}
	// Kontot har bara ett fönster på codex-kvoten, och då ska inget hittas på.
	if lage.FemTimmar != nil {
		t.Fatalf("femtimmarsfönstret fanns inte i svaret: %+v", lage.FemTimmar)
	}
	if lage.Kvottyp != "codex" {
		t.Fatalf("kvottypen kom fel: %q", lage.Kvottyp)
	}
	if lage.AvlastAt == 0 {
		t.Fatal("avläsningstiden sattes inte")
	}
	for _, krav := range []string{"initialize", "account/rateLimits/read"} {
		if !strings.Contains(sedd, krav) {
			t.Errorf("anropet saknar %q", krav)
		}
	}
}

// Fönstren identifieras av sin varaktighet, inte av att de heter primary eller
// secondary. Vilket som är primary skiljer mellan konton.
func TestCodexKvotMapparFonsterPaVaraktighet(t *testing.T) {
	svar := `{"id":2,"result":{"rateLimits":{"limitId":"codex_bengalfox",` +
		`"primary":{"usedPercent":12,"windowDurationMins":300,"resetsAt":1789347232},` +
		`"secondary":{"usedPercent":34,"windowDurationMins":10080,"resetsAt":1789934032}}}}`
	medCodexKorare(t, func(context.Context, string, string) ([]byte, error) { return []byte(svar), nil })

	lage := CodexKvot(context.Background(), "codex")
	if lage == nil || lage.FemTimmar == nil || lage.SjuDagar == nil {
		t.Fatalf("båda fönstren skulle läsas: %+v", lage)
	}
	if lage.FemTimmar.Andel != 0.12 || lage.SjuDagar.Andel != 0.34 {
		t.Fatalf("fönstren hamnade fel: fem %+v, sju %+v", lage.FemTimmar, lage.SjuDagar)
	}
}

// Kvoten är en trevlighet i vyn. Går avläsningen fel ska PM lämna den gamla
// siffran i fred, inte skriva något halvt.
func TestCodexKvotGerIngetVidFel(t *testing.T) {
	medCodexKorare(t, func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("codex saknas")
	})
	if lage := CodexKvot(context.Background(), "codex"); lage != nil {
		t.Fatalf("ett fel ska ge nil, fick %+v", lage)
	}

	for _, skrap := range []string{"", "inte json alls", `{"id":2,"result":{}}`,
		`{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":140,"windowDurationMins":300}}}}`} {
		medCodexKorare(t, func(context.Context, string, string) ([]byte, error) { return []byte(skrap), nil })
		if lage := CodexKvot(context.Background(), "codex"); lage != nil {
			t.Errorf("svaret %q skulle ge nil, gav %+v", skrap, lage)
		}
	}

	// Utan kommando startar ingen process alls.
	medCodexKorare(t, func(context.Context, string, string) ([]byte, error) {
		t.Fatal("app-servern startades trots att kommandot saknas")
		return nil, nil
	})
	if lage := CodexKvot(context.Background(), "  "); lage != nil {
		t.Fatalf("tomt kommando ska ge nil, fick %+v", lage)
	}
}
