package pm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRepoLasTarOverVidFelStarttid(t *testing.T) {
	ws := t.TempDir()
	repo := t.TempDir()
	las := NyRepoLas(ws, repo)
	skrivLasfil(t, las, lasInnehall{
		PID:      os.Getpid(),
		Starttid: "fel starttid",
		Korning:  "gammal",
	})

	if _, lever, err := las.Agare(); err != nil || lever {
		t.Fatalf("låset med fel starttid skulle vara dött, fick %v %v", lever, err)
	}
	if tagen, err := las.Ta(repo, "ny"); err != nil || !tagen {
		t.Fatalf("PM skulle ta över låset, fick %v %v", tagen, err)
	}
}

func TestRepoLasTarOverUtanStarttid(t *testing.T) {
	ws := t.TempDir()
	repo := t.TempDir()
	las := NyRepoLas(ws, repo)
	skrivLasfil(t, las, lasInnehall{
		PID:     os.Getpid(),
		Korning: "gammal",
	})

	if _, lever, err := las.Agare(); err != nil || lever {
		t.Fatalf("låset utan starttid skulle vara dött, fick %v %v", lever, err)
	}
	if tagen, err := las.Ta(repo, "ny"); err != nil || !tagen {
		t.Fatalf("PM skulle ta över det gamla låset, fick %v %v", tagen, err)
	}
}

func skrivLasfil(t *testing.T, las *RepoLas, innehall lasInnehall) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(las.Sokvag()), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(innehall)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(las.Sokvag(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepoLasBehallerLasetNarPsSaknas(t *testing.T) {
	// Går ps inte att köra vet PM ingenting om ägaren. Då ska låset stå kvar.
	t.Setenv("PATH", t.TempDir())
	ws := t.TempDir()
	repo := t.TempDir()
	las := NyRepoLas(ws, repo)
	skrivLasfil(t, las, lasInnehall{
		PID:      os.Getpid(),
		Starttid: "någon starttid",
		Korning:  "pågående",
	})

	_, lever, err := las.Agare()
	if err != nil {
		t.Fatal(err)
	}
	if !lever {
		t.Fatal("PM kallade låset dött trots att den inte kunde fråga ps")
	}
}

func TestRepoLasSlapperLasetNarProcessenSaknas(t *testing.T) {
	ws := t.TempDir()
	repo := t.TempDir()
	las := NyRepoLas(ws, repo)
	// Ett pid som garanterat inte finns. ps svarar med exitkod 1.
	skrivLasfil(t, las, lasInnehall{PID: 2147483600, Starttid: "någon starttid"})

	_, lever, err := las.Agare()
	if err != nil {
		t.Fatal(err)
	}
	if lever {
		t.Fatal("PM kallade ett lås utan process levande")
	}
}
