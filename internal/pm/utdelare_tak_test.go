package pm

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockeradKorare struct {
	startad chan struct{}
	slapp   chan struct{}
	mu      sync.Mutex
	aktiva  int
	flest   int
}

func (k *blockeradKorare) Namn() string { return "blockerad" }

func (k *blockeradKorare) Fraga(context.Context, string) (string, error) { return "", nil }

func (k *blockeradKorare) Kor(ctx context.Context, in KorInput) (Resultat, error) {
	k.mu.Lock()
	k.aktiva++
	if k.aktiva > k.flest {
		k.flest = k.aktiva
	}
	k.mu.Unlock()
	k.startad <- struct{}{}
	select {
	case <-k.slapp:
	case <-ctx.Done():
		return Resultat{ExitKod: 1, Logg: in.Logg}, ctx.Err()
	}
	k.mu.Lock()
	k.aktiva--
	k.mu.Unlock()
	return Resultat{Utdata: "klar", Logg: in.Logg}, nil
}

func TestMaxSamtidigaKoarOchStartarNarPlatsBlirLedig(t *testing.T) {
	db := testDB(t)
	ws := t.TempDir()
	korare := &blockeradKorare{startad: make(chan struct{}, 3), slapp: make(chan struct{}, 3)}
	konfig := StandardKonfig()
	konfig.DefaultAgent = korare.Namn()
	konfig.MaxSamtidiga = 2
	konfig.Agenter[korare.Namn()] = AgentKonfig{Kommando: "true", Args: []string{"{brief}"}, Brief: "arg", Svar: "stdout"}
	register := NewAgentRegister()
	register.RegistreraKorare(korare)
	register.SattForval(korare.Namn())
	utdelare := NewUtdelare(db, konfig, register)
	utdelare.Vanteintervall = 5 * time.Millisecond

	tasks := make([]string, 3)
	for i := range tasks {
		projektID := projektMedRepo(t, db, string(rune('a'+i)), t.TempDir())
		tasks[i] = testTask(t, db, projektID, "Task", "text", 20+i)
	}
	fel := make(chan error, 3)
	for _, taskID := range tasks {
		go func() {
			_, err := utdelare.DelaUt(context.Background(), UtdelInput{TaskID: taskID, WorkspaceDir: ws, KoTimeout: 5 * time.Second})
			fel <- err
		}()
	}

	vantaPaStart(t, korare.startad)
	vantaPaStart(t, korare.startad)
	select {
	case <-korare.startad:
		t.Fatal("tre agentkörningar startade trots taket 2")
	case <-time.After(100 * time.Millisecond):
	}
	var koade, korande int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_korningar WHERE status=?`, StatusKoad).Scan(&koade); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_korningar WHERE status=?`, StatusKor).Scan(&korande); err != nil {
		t.Fatal(err)
	}
	if koade != 1 || korande != 2 {
		t.Fatalf("väntade en köad och två körande, fick %d och %d", koade, korande)
	}

	korare.slapp <- struct{}{}
	vantaPaStart(t, korare.startad)
	korare.slapp <- struct{}{}
	korare.slapp <- struct{}{}
	for range tasks {
		if err := <-fel; err != nil {
			t.Fatalf("utdelning misslyckades: %v", err)
		}
	}
	korare.mu.Lock()
	flest := korare.flest
	korare.mu.Unlock()
	if flest != 2 {
		t.Fatalf("högst två skulle köra samtidigt, fick %d", flest)
	}
}

func vantaPaStart(t *testing.T, startad <-chan struct{}) {
	t.Helper()
	select {
	case <-startad:
	case <-time.After(5 * time.Second):
		t.Fatal("agentkörningen startade inte")
	}
}

type paniskKorare struct{}

func (paniskKorare) Namn() string                                  { return "panisk" }
func (paniskKorare) Fraga(context.Context, string) (string, error) { return "", nil }
func (paniskKorare) Kor(context.Context, KorInput) (Resultat, error) {
	panic("agenten sprack")
}

func TestPanikILopandeKorningSlapperPlatsen(t *testing.T) {
	db := testDB(t)
	ws := t.TempDir()
	konfig := StandardKonfig()
	konfig.MaxSamtidiga = 1
	konfig.DefaultAgent = "panisk"
	konfig.Agenter["panisk"] = AgentKonfig{Kommando: "true", Args: []string{"{brief}"}, Brief: "arg", Svar: "stdout"}
	register := NewAgentRegister()
	register.RegistreraKorare(paniskKorare{})
	register.SattForval("panisk")
	utdelare := NewUtdelare(db, konfig, register)

	projektID := projektMedRepo(t, db, "panik", t.TempDir())
	taskID := testTask(t, db, projektID, "Task", "text", 90)

	func() {
		defer func() {
			if orsak := recover(); orsak == nil {
				t.Error("körningen paniserade inte som testet förutsätter")
			}
		}()
		_, _ = utdelare.DelaUt(context.Background(), UtdelInput{TaskID: taskID, WorkspaceDir: ws, KoTimeout: 2 * time.Second})
	}()

	var kvar int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_korningar WHERE status IN (?,?)`, StatusKoad, StatusKor).Scan(&kvar); err != nil {
		t.Fatal(err)
	}
	if kvar != 0 {
		t.Fatalf("platsen släpptes inte efter paniken, %d körningar står kvar", kvar)
	}
}
