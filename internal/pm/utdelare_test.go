package pm

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/timeutil"
)

// fejkKorare ersätter en riktig agent i testerna.
type fejkKorare struct {
	namn    string
	exitKod int
	utdata  string
	felet   error
	sedd    KorInput
	drojer  time.Duration
	mu      sync.Mutex
	anrop   int
}

func (f *fejkKorare) Namn() string { return f.namn }

func (f *fejkKorare) Fraga(ctx context.Context, prompt string) (string, error) { return f.utdata, nil }

func (f *fejkKorare) Kor(ctx context.Context, in KorInput) (Resultat, error) {
	f.mu.Lock()
	f.sedd = in
	f.anrop++
	f.mu.Unlock()
	if f.drojer > 0 {
		time.Sleep(f.drojer)
	}
	return Resultat{Utdata: f.utdata, ExitKod: f.exitKod, Logg: in.Logg}, f.felet
}

func (f *fejkKorare) briefen() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sedd.Brief
}

func utdelareMed(t *testing.T, db *sql.DB, korare *fejkKorare) *Utdelare {
	t.Helper()
	k := StandardKonfig()
	k.DefaultAgent = korare.namn
	k.Agenter[korare.namn] = AgentKonfig{Kommando: "true", Args: []string{"{brief}"}, Brief: "arg", Svar: "stdout"}
	reg := NewAgentRegister()
	reg.RegistreraKorare(korare)
	reg.SattForval(korare.namn)
	u := NewUtdelare(db, k, reg)
	u.Vanteintervall = 10 * time.Millisecond
	return u
}

func testTask(t *testing.T, db *sql.DB, projectID, titel, beskrivning string, seq int) string {
	t.Helper()
	id := ids.New()
	nu := timeutil.Now()
	_, err := db.Exec(`INSERT INTO tasks(id, project_id, title, description, type, status, priority, task_seq, created_at, updated_at)
	                   VALUES(?,?,?,?,'task','todo',3,?,?,?)`, id, projectID, titel, beskrivning, seq, nu, nu)
	if err != nil {
		t.Fatalf("skapa task: %v", err)
	}
	return id
}

func projektMedRepo(t *testing.T, db *sql.DB, alias, repo string) string {
	t.Helper()
	id := ids.New()
	nu := timeutil.Now()
	if _, err := db.Exec(`INSERT INTO projects(id, alias, name, repo_path, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		id, alias, alias, repo, nu, nu); err != nil {
		t.Fatalf("skapa projekt: %v", err)
	}
	return id
}

func TestDelaUtLyckadKorningStangerTasken(t *testing.T) {
	db := testDB(t)
	ws := t.TempDir()
	repo := t.TempDir()
	pid := projektMedRepo(t, db, "demo", repo)
	taskID := testTask(t, db, pid, "Fixa tråd-vyn", "Beskrivningen som agenten ska få.", 7)

	if _, err := db.Exec(`INSERT INTO project_memory(id, project_id, body, tags, actor_kind, actor_name, created_at)
	                      VALUES(?,?,?,'','human','rasmus',?)`, ids.New(), pid, "PM-databasen är skild från vardagsdatabasen.", timeutil.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSamtalStore(db).Add(context.Background(), pid, "", models.Actor{Kind: models.ActorKindHuman, Name: "rasmus"}, "kör den här nu"); err != nil {
		t.Fatal(err)
	}

	korare := &fejkKorare{namn: "fejk", utdata: "klart, allt byggt"}
	korning, err := utdelareMed(t, db, korare).DelaUt(context.Background(), UtdelInput{TaskID: taskID, WorkspaceDir: ws})
	if err != nil {
		t.Fatalf("dela-ut: %v", err)
	}
	if korning.Status != StatusKlar || korning.ExitKod == nil || *korning.ExitKod != 0 {
		t.Fatalf("väntade klar med exitkod 0, fick %+v", korning)
	}

	// Briefen ska bära beskrivning, memory och samtalsinlägg.
	for _, vantat := range []string{"Beskrivningen som agenten ska få", "PM-databasen är skild", "kör den här nu", "TASK-7"} {
		if !strings.Contains(korare.briefen(), vantat) {
			t.Fatalf("briefen saknar %q:\n%s", vantat, korare.briefen())
		}
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM tasks WHERE id=?`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("tasken skulle vara done, är %q", status)
	}
	kommentar, aktor := sistaKommentar(t, db, taskID)
	if !strings.Contains(kommentar, "klart, allt byggt") || aktor != "fejk" {
		t.Fatalf("kommentaren ska komma från ai:fejk med agentens svar, fick %q av %q", kommentar, aktor)
	}
}

func TestDelaUtMisslyckadKorningLamnarTaskenITodo(t *testing.T) {
	db := testDB(t)
	ws := t.TempDir()
	pid := projektMedRepo(t, db, "demo", t.TempDir())
	taskID := testTask(t, db, pid, "Omöjlig task", "kan inte lösas", 8)

	korare := &fejkKorare{namn: "fejk", exitKod: 3, utdata: "gick inte"}
	korning, err := utdelareMed(t, db, korare).DelaUt(context.Background(), UtdelInput{TaskID: taskID, WorkspaceDir: ws})
	if err != nil {
		t.Fatalf("dela-ut ska rapportera resultatet utan att fela hårt: %v", err)
	}
	if korning.Status != StatusFel || *korning.ExitKod != 3 {
		t.Fatalf("väntade fel med exitkod 3, fick %+v", korning)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM tasks WHERE id=?`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "todo" {
		t.Fatalf("tasken skulle vara tillbaka i todo, är %q", status)
	}
	kommentar, aktor := sistaKommentar(t, db, taskID)
	if !strings.Contains(kommentar, "misslyckades") || !strings.Contains(kommentar, "exitkod 3") || aktor != "fejk" {
		t.Fatalf("felkommentaren saknar besked: %q av %q", kommentar, aktor)
	}
}

// Andra körningen mot samma repo köas medan den första håller låset.
func TestAndraKorningenKoasOchStartarSedan(t *testing.T) {
	db := testDB(t)
	ws := t.TempDir()
	repo := t.TempDir()
	pid := projektMedRepo(t, db, "demo", repo)
	forst := testTask(t, db, pid, "Först", "en", 9)
	sedan := testTask(t, db, pid, "Sedan", "två", 10)

	korare := &fejkKorare{namn: "fejk", utdata: "ok", drojer: 300 * time.Millisecond}
	u := utdelareMed(t, db, korare)

	klart := make(chan struct{})
	go func() {
		defer close(klart)
		if _, err := u.DelaUt(context.Background(), UtdelInput{TaskID: forst, WorkspaceDir: ws}); err != nil {
			t.Errorf("första körningen: %v", err)
		}
	}()

	// Vänta tills första körningen tagit låset.
	las := NyRepoLas(ws, repo)
	for i := 0; i < 200; i++ {
		if _, lever, _ := las.Agare(); lever {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if besked := las.LasBesked(); !strings.Contains(besked, "används av körning") {
		t.Fatalf("låset skulle vara taget, fick %q", besked)
	}

	andra, err := u.DelaUt(context.Background(), UtdelInput{TaskID: sedan, WorkspaceDir: ws, KoTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("andra körningen: %v", err)
	}
	<-klart

	if andra.Status != StatusKlar {
		t.Fatalf("andra körningen skulle bli klar efter kön, fick %q", andra.Status)
	}
	if korare.anrop != 2 {
		t.Fatalf("båda körningarna skulle ha kört, fick %d anrop", korare.anrop)
	}
	if _, lever, _ := las.Agare(); lever {
		t.Fatal("låset skulle vara släppt efteråt")
	}
}

func TestNekaGerBeskedIStalletForKo(t *testing.T) {
	db := testDB(t)
	ws := t.TempDir()
	repo := t.TempDir()
	pid := projektMedRepo(t, db, "demo", repo)
	taskID := testTask(t, db, pid, "Task", "text", 11)

	// Simulera en pågående körning i den här processen.
	las := NyRepoLas(ws, repo)
	if tagen, err := las.Ta(repo, "annan-korning"); err != nil || !tagen {
		t.Fatalf("kunde inte ta låset: %v", err)
	}
	defer las.Slapp()

	korare := &fejkKorare{namn: "fejk", utdata: "ok"}
	_, err := utdelareMed(t, db, korare).DelaUt(context.Background(), UtdelInput{TaskID: taskID, WorkspaceDir: ws, Neka: true})
	if err == nil || !strings.Contains(err.Error(), "repot är upptaget") {
		t.Fatalf("väntade besked om upptaget repo, fick %v", err)
	}
	if korare.anrop != 0 {
		t.Fatal("agenten skulle inte ha körts")
	}
}

// Ett lås efter en död process ska tas över, inte blockera för alltid.
func TestInaktuelltLasTasOver(t *testing.T) {
	ws := t.TempDir()
	repo := t.TempDir()
	las := NyRepoLas(ws, repo)
	if err := os.MkdirAll(filepath.Dir(las.Sokvag()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(las.Sokvag(), []byte(`{"pid":999999,"korning":"gammal","repo":"x","sedan":"igår"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(las.LasBesked(), "inaktuellt lås") {
		t.Fatalf("väntade besked om inaktuellt lås, fick %q", las.LasBesked())
	}
	tagen, err := las.Ta(repo, "ny")
	if err != nil || !tagen {
		t.Fatalf("inaktuellt lås skulle tas över, fick %v %v", tagen, err)
	}
}

// En körning vars process är borta får inte ligga kvar som koad.
func TestOvergivenKorningStadasTillFel(t *testing.T) {
	db := testDB(t)
	pid := projektMedRepo(t, db, "demo", t.TempDir())
	taskID := testTask(t, db, pid, "Task", "text", 12)
	store := NewKorningStore(db)
	k := &Korning{ProjectID: pid, TaskID: taskID, Agent: "fejk", Status: StatusKoad}
	if err := store.Skapa(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE pm_korningar SET pid=999999 WHERE id=?`, k.ID); err != nil {
		t.Fatal(err)
	}
	antal, err := store.StadaOvergivna(context.Background())
	if err != nil || antal != 1 {
		t.Fatalf("väntade en städad körning, fick %d %v", antal, err)
	}
	efter, err := store.Hamta(context.Background(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if efter.Status != StatusFel {
		t.Fatalf("övergiven körning skulle bli fel, är %q", efter.Status)
	}
}

func sistaKommentar(t *testing.T, db *sql.DB, taskID string) (string, string) {
	t.Helper()
	var body, aktor string
	err := db.QueryRow(`SELECT body, actor_name FROM comments WHERE task_id=? ORDER BY created_at DESC LIMIT 1`, taskID).Scan(&body, &aktor)
	if err != nil {
		t.Fatalf("läs kommentar: %v", err)
	}
	return body, aktor
}

// Kroken måste peka på PM-workspacet. Utan det kör ett backlog-baserat
// verktyg som arbetar mot vardagsdatabasen och skriver på fel task.
func TestKrokenKorMotPMProfilen(t *testing.T) {
	db := testDB(t)
	ws := t.TempDir()
	repo := t.TempDir()
	pid := projektMedRepo(t, db, "demo", repo)
	taskID := testTask(t, db, pid, "Task med krok", "text", 13)

	spar := filepath.Join(ws, "krok.txt")
	skript := filepath.Join(ws, "krok.sh")
	innehall := "#!/bin/sh\n{\n  echo \"args: $*\"\n  echo \"BACKLOG_PROFILE=$BACKLOG_PROFILE\"\n  echo \"BACKLOG_DB=$BACKLOG_DB\"\n  echo \"EGEN=$EGEN\"\n} >> " + spar + "\n"
	if err := os.WriteFile(skript, []byte(innehall), 0o755); err != nil {
		t.Fatal(err)
	}

	korare := &fejkKorare{namn: "fejk", utdata: "ok"}
	u := utdelareMed(t, db, korare)
	u.konfig.Krok = Krok{
		Anspraka: []string{skript, "anspraka", "{task}", "{repo}", "{aktor}"},
		Slapp:    []string{skript, "slapp", "{task}"},
		Miljo:    map[string]string{"EGEN": "värde"},
	}

	if _, err := u.DelaUt(context.Background(), UtdelInput{TaskID: taskID, WorkspaceDir: ws, Profil: "pm"}); err != nil {
		t.Fatalf("dela-ut: %v", err)
	}

	data, err := os.ReadFile(spar)
	if err != nil {
		t.Fatalf("kroken kördes inte: %v", err)
	}
	text := string(data)
	for _, vantat := range []string{"args: anspraka TASK-13", "args: slapp TASK-13", "BACKLOG_PROFILE=pm", "EGEN=värde", "ai:fejk"} {
		if !strings.Contains(text, vantat) {
			t.Fatalf("kroken saknar %q:\n%s", vantat, text)
		}
	}
	if !strings.Contains(text, "BACKLOG_DB="+filepath.Join(ws, "backlog.db")) {
		t.Fatalf("kroken pekar inte på PM-databasen:\n%s", text)
	}
}

// Kön ska fungera utan krok - låsningen är PM:s egen.
func TestKonFungerarUtanKrok(t *testing.T) {
	db := testDB(t)
	ws := t.TempDir()
	repo := t.TempDir()
	pid := projektMedRepo(t, db, "demo", repo)
	taskID := testTask(t, db, pid, "Utan krok", "text", 14)

	korare := &fejkKorare{namn: "fejk", utdata: "ok"}
	u := utdelareMed(t, db, korare)
	if len(u.konfig.Krok.Anspraka) != 0 {
		t.Fatal("standardkonfigurationen ska inte ha någon krok")
	}
	k, err := u.DelaUt(context.Background(), UtdelInput{TaskID: taskID, WorkspaceDir: ws})
	if err != nil || k.Status != StatusKlar {
		t.Fatalf("körning utan krok ska bli klar, fick %+v %v", k, err)
	}
}

// Utdelningen vinner över regeln, och regeln över agentens förval.
func TestModellValjsIRattOrdning(t *testing.T) {
	agent := AgentKonfig{Kommando: "true", Args: []string{"{brief}"}, Brief: "arg", Svar: "stdout", Modell: "forval"}
	k := Konfig{
		DefaultAgent: "a",
		Agenter:      map[string]AgentKonfig{"a": agent},
		Regler:       []Regel{{Namn: "buggar", Typ: []string{"bug"}, Agent: "a", Modell: "regelmodell"}},
	}
	val, err := ValjAgent(k, TaskFakta{Typ: "bug"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if val.Modell != "regelmodell" {
		t.Fatalf("regeln bar inte sin modell: %+v", val)
	}
	if got := forstaIckeTomma("", val.Modell, agent.Modell); got != "regelmodell" {
		t.Fatalf("regeln skulle vinna över förvalet, fick %q", got)
	}
	if got := forstaIckeTomma("handplockad", val.Modell, agent.Modell); got != "handplockad" {
		t.Fatalf("utdelningen skulle vinna, fick %q", got)
	}
	utanRegel, err := ValjAgent(Konfig{DefaultAgent: "a", Agenter: k.Agenter}, TaskFakta{Typ: "task"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := forstaIckeTomma("", utanRegel.Modell, agent.Modell); got != "forval" {
		t.Fatalf("agentens förval skulle gälla, fick %q", got)
	}
}
