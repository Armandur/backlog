package pm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mazen160/backlog/internal/timeutil"
)

const (
	testserverStopptid     = 10 * time.Second
	testserverHalsotimeout = 2 * time.Second
	testserverCachetid     = 3 * time.Second

	TestserverNere    = "nere"
	TestserverStartar = "startar"
	TestserverUppe    = "uppe"
	TestserverKrasch  = "krasch"
)

// Testserver beskriver en process som PM har startat och därför får stoppa.
type Testserver struct {
	Alias     string `json:"alias"`
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartadAt int64  `json:"startad_at"`
	Logg      string `json:"logg_sokvag"`
	Lever     bool   `json:"lever"`
	Status    string `json:"status"`
	Exitkod   *int   `json:"exitkod,omitempty"`
}

// TestserverStore äger testserverprocesser och deras databasrader.
type TestserverStore struct {
	db        *sql.DB
	konfig    Konfig
	workspace string
	stopptid  time.Duration
	halsotid  time.Duration
	cachetid  time.Duration
}

type testserverHalsoNyckel struct {
	db           *sql.DB
	alias, halsa string
	pid, port    int
}

type testserverHalsoSvar struct {
	klar       chan struct{}
	uppe       bool
	giltigTill time.Time
}

var testserverHalsocache = struct {
	sync.Mutex
	svar map[testserverHalsoNyckel]*testserverHalsoSvar
}{svar: make(map[testserverHalsoNyckel]*testserverHalsoSvar)}

var testserverExitkoder sync.Map

func NewTestserverStore(db *sql.DB, konfig Konfig, workspace string) *TestserverStore {
	return &TestserverStore{
		db: db, konfig: konfig, workspace: workspace, stopptid: testserverStopptid,
		halsotid: testserverHalsotimeout, cachetid: testserverCachetid,
	}
}

// Starta reserverar porten, startar en processgrupp och sparar dess ägarskap.
func (s *TestserverStore) Starta(ctx context.Context, alias string) (*Testserver, error) {
	serverKonfig, repoPath, err := s.underlag(ctx, alias)
	if err != nil {
		return nil, err
	}
	if befintlig, err := s.Hamta(ctx, alias); err == nil {
		if befintlig.Lever {
			return nil, fmt.Errorf("testservern för %q kör redan på port %d. Stoppa den först om du vill starta om den", alias, befintlig.Port)
		}
		if err := s.taBortRad(ctx, befintlig); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	intervall := PortIntervall{Fran: s.konfig.Portar.Fran, Till: s.konfig.Portar.Till}
	reservation, err := NewPortStore(s.db).Reservera(ctx, alias, os.Getpid(), intervall, serverKonfig.Port)
	if err != nil {
		return nil, err
	}
	reserverad := true
	defer func() {
		if reserverad {
			_, _ = s.db.ExecContext(context.Background(),
				`DELETE FROM pm_portar WHERE port=? AND projekt=? AND pid=?`, reservation.Port, alias, os.Getpid())
		}
	}()

	logg := TestserverLoggfil(s.workspace, alias)
	if err := os.MkdirAll(filepath.Dir(logg), 0o755); err != nil {
		return nil, fmt.Errorf("kunde inte skapa testserverns loggkatalog: %w. Kontrollera att PM får skriva i workspace-katalogen", err)
	}
	if err := roteraTestserverlogg(logg); err != nil {
		return nil, err
	}
	loggfil, err := os.OpenFile(logg, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("kunde inte öppna testserverns logg: %w. Kontrollera att PM får skriva i workspace-katalogen", err)
	}
	defer loggfil.Close()

	nu := timeutil.Now()
	resultat, err := s.db.ExecContext(ctx,
		`INSERT INTO pm_testservrar(alias,pid,port,startad_at,logg_sokvag) VALUES(?,?,?,?,?)
		 ON CONFLICT(alias) DO NOTHING`, alias, 0, reservation.Port, nu, logg)
	if err != nil {
		return nil, fmt.Errorf("kunde inte spara testservern: %w", err)
	}
	antal, err := resultat.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("kunde inte kontrollera testserverns start: %w", err)
	}
	if antal == 0 {
		return nil, fmt.Errorf("en annan process hann starta testservern för %q först. Läs status igen", alias)
	}
	radSparad := true
	defer func() {
		if radSparad {
			_, _ = s.db.ExecContext(context.Background(),
				`DELETE FROM pm_testservrar WHERE alias=? AND pid=0`, alias)
		}
	}()

	args := ersattPort(serverKonfig.Args, reservation.Port)
	kommando := exec.Command(serverKonfig.Kommando, args...)
	kommando.Dir = serverKonfig.CWD
	if kommando.Dir == "" {
		kommando.Dir = repoPath
	}
	kommando.Env = miljo(serverKonfig.Miljo)
	kommando.Stdout = loggfil
	kommando.Stderr = loggfil
	kommando.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := kommando.Start(); err != nil {
		return nil, fmt.Errorf("kunde inte starta testservern för %q: %w. Kontrollera kommandot under Konfig", alias, err)
	}
	pid := kommando.Process.Pid
	exitfil := testserverExitfil(logg, pid)
	testserverExitkoder.Delete(exitfil)
	if err := os.Remove(exitfil); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = kommando.Wait()
		return nil, fmt.Errorf("kunde inte rensa testserverns gamla exitkod: %w", err)
	}
	go func() {
		_ = kommando.Wait()
		if kommando.ProcessState == nil {
			return
		}
		exitkod := kommando.ProcessState.ExitCode()
		testserverExitkoder.Store(exitfil, exitkod)
		_ = os.WriteFile(exitfil, []byte(strconv.Itoa(exitkod)+"\n"), 0o600)
	}()
	if err := s.kopplaProcess(ctx, alias, reservation.Port, pid); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return nil, err
	}

	reserverad = false
	radSparad = false
	return &Testserver{
		Alias: alias, PID: pid, Port: reservation.Port, StartadAt: nu,
		Logg: logg, Lever: true, Status: TestserverStartar,
	}, nil
}

// TestserverLoggfil ger sökvägen till ett projekts testserverlogg.
func TestserverLoggfil(workspace, alias string) string {
	return filepath.Join(workspace, "loggar", "testserver-"+alias+".log")
}

func roteraTestserverlogg(logg string) error {
	if err := os.Rename(logg, logg+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("kunde inte rotera testserverns logg: %w", err)
	}
	return nil
}

// Status kontrollerar både processen och om dess HTTP-port svarar.
func (s *TestserverStore) Status(ctx context.Context, alias string) (*Testserver, error) {
	server, err := s.Hamta(ctx, alias)
	if errors.Is(err, sql.ErrNoRows) {
		return &Testserver{Alias: alias, Status: TestserverNere}, nil
	}
	if err != nil {
		return nil, err
	}
	if !server.Lever {
		server.Status = TestserverKrasch
		server.Exitkod = lasTestserverExitkod(server.Logg, server.PID)
		return server, nil
	}
	halsa := s.konfig.Testserver[alias].Halsa
	if halsa == "" {
		halsa = "/"
	}
	if s.halsaSvarar(ctx, server, halsa) {
		server.Status = TestserverUppe
	} else {
		server.Status = TestserverStartar
	}
	return server, nil
}

// Hamta läser processraden utan att göra en hälsokontroll.
func (s *TestserverStore) Hamta(ctx context.Context, alias string) (*Testserver, error) {
	var server Testserver
	err := s.db.QueryRowContext(ctx,
		`SELECT alias,pid,port,startad_at,logg_sokvag FROM pm_testservrar WHERE alias=?`, alias).
		Scan(&server.Alias, &server.PID, &server.Port, &server.StartadAt, &server.Logg)
	if err != nil {
		return nil, err
	}
	server.Lever = processgruppLever(server.PID)
	return &server, nil
}

// Stoppa signalerar bara en process som fortfarande har samma databasrad.
func (s *TestserverStore) Stoppa(ctx context.Context, alias string) (*Testserver, error) {
	server, err := s.Hamta(ctx, alias)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("testservern för %q kör inte, så det finns inget att stoppa", alias)
	}
	if err != nil {
		return nil, err
	}
	if server.Lever {
		if err := s.signaleraRegistrerad(ctx, alias, server.PID, syscall.SIGTERM); err != nil {
			return nil, err
		}
		slut := time.Now().Add(s.stopptid)
		for processgruppLever(server.PID) && time.Now().Before(slut) {
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		if processgruppLever(server.PID) {
			if err := s.signaleraRegistrerad(ctx, alias, server.PID, syscall.SIGKILL); err != nil {
				return nil, err
			}
			killSlut := time.Now().Add(time.Second)
			for processgruppLever(server.PID) && time.Now().Before(killSlut) {
				timer := time.NewTimer(20 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
			if processgruppLever(server.PID) {
				return nil, fmt.Errorf("testservern för %q svarar inte på stoppsignalen. Vänta en stund och försök igen", alias)
			}
		}
	}
	if err := s.taBortRad(ctx, server); err != nil {
		return nil, err
	}
	server.Lever = false
	server.Status = TestserverNere
	return server, nil
}

func (s *TestserverStore) halsaSvarar(ctx context.Context, server *Testserver, halsa string) bool {
	if !strings.HasPrefix(halsa, "/") {
		halsa = "/" + halsa
	}
	nyckel := testserverHalsoNyckel{
		db: s.db, alias: server.Alias, pid: server.PID, port: server.Port, halsa: halsa,
	}
	for {
		nu := time.Now()
		testserverHalsocache.Lock()
		for gammalNyckel, gammaltSvar := range testserverHalsocache.svar {
			select {
			case <-gammaltSvar.klar:
				if !nu.Before(gammaltSvar.giltigTill) {
					delete(testserverHalsocache.svar, gammalNyckel)
				}
			default:
			}
		}
		befintligt := testserverHalsocache.svar[nyckel]
		if befintligt != nil {
			select {
			case <-befintligt.klar:
				if nu.Before(befintligt.giltigTill) {
					uppe := befintligt.uppe
					testserverHalsocache.Unlock()
					return uppe
				}
				delete(testserverHalsocache.svar, nyckel)
			default:
				klar := befintligt.klar
				testserverHalsocache.Unlock()
				select {
				case <-ctx.Done():
					return false
				case <-klar:
					continue
				}
			}
		}
		svar := &testserverHalsoSvar{klar: make(chan struct{})}
		testserverHalsocache.svar[nyckel] = svar
		testserverHalsocache.Unlock()

		klient := &http.Client{
			Timeout: s.halsotid,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		adress := fmt.Sprintf("http://localhost:%d%s", server.Port, halsa)
		begaran, err := http.NewRequestWithContext(ctx, http.MethodGet, adress, nil)
		uppe := false
		if err == nil {
			if httpSvar, anropsfel := klient.Do(begaran); anropsfel == nil {
				uppe = true
				httpSvar.Body.Close()
			}
		}

		testserverHalsocache.Lock()
		svar.uppe = uppe
		svar.giltigTill = time.Now().Add(s.cachetid)
		close(svar.klar)
		testserverHalsocache.Unlock()
		return uppe
	}
}

func testserverExitfil(logg string, pid int) string {
	return fmt.Sprintf("%s.%d.exitkod", logg, pid)
}

func lasTestserverExitkod(logg string, pid int) *int {
	exitfil := testserverExitfil(logg, pid)
	if sparad, finns := testserverExitkoder.Load(exitfil); finns {
		exitkod := sparad.(int)
		return &exitkod
	}
	data, err := os.ReadFile(exitfil)
	if err != nil {
		return nil
	}
	exitkod, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil
	}
	return &exitkod
}

// StadaDoda tar bort rader vars processgrupp inte längre finns.
func (s *TestserverStore) StadaDoda(ctx context.Context) (int, error) {
	rader, err := s.db.QueryContext(ctx,
		`SELECT alias,pid,port,startad_at,logg_sokvag FROM pm_testservrar`)
	if err != nil {
		return 0, fmt.Errorf("kunde inte läsa testservrarna: %w", err)
	}
	defer rader.Close()
	var doda []*Testserver
	for rader.Next() {
		server := &Testserver{}
		if err := rader.Scan(&server.Alias, &server.PID, &server.Port, &server.StartadAt, &server.Logg); err != nil {
			return 0, err
		}
		if !processgruppLever(server.PID) {
			doda = append(doda, server)
		}
	}
	if err := rader.Err(); err != nil {
		return 0, err
	}
	for _, server := range doda {
		if err := s.taBortRad(ctx, server); err != nil {
			return 0, err
		}
	}
	return len(doda), nil
}

func (s *TestserverStore) kopplaProcess(ctx context.Context, alias string, port, pid int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	rad, err := tx.ExecContext(ctx, `UPDATE pm_testservrar SET pid=? WHERE alias=? AND pid=0`, pid, alias)
	if err != nil {
		return fmt.Errorf("kunde inte spara testserverns pid: %w", err)
	}
	antal, err := rad.RowsAffected()
	if err != nil || antal != 1 {
		return fmt.Errorf("testserverns startrad försvann innan PM hann spara processen. Försök igen")
	}
	reservation, err := tx.ExecContext(ctx,
		`UPDATE pm_portar SET pid=? WHERE port=? AND projekt=? AND pid=?`, pid, port, alias, os.Getpid())
	if err != nil {
		return fmt.Errorf("kunde inte koppla porten till testservern: %w", err)
	}
	antal, err = reservation.RowsAffected()
	if err != nil || antal != 1 {
		return fmt.Errorf("testserverns portreservation försvann innan PM hann spara processen. Försök igen")
	}
	return tx.Commit()
}

func (s *TestserverStore) signaleraRegistrerad(ctx context.Context, alias string, pid int, signal syscall.Signal) error {
	var registreradPID int
	if err := s.db.QueryRowContext(ctx, `SELECT pid FROM pm_testservrar WHERE alias=?`, alias).Scan(&registreradPID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("PM hittar ingen registrerad process för testservern och stoppade därför ingen. Läs status igen")
		}
		return err
	}
	if registreradPID != pid {
		return fmt.Errorf("en annan process har tagit över numret PM sparade, så PM stoppade ingen. Läs status igen")
	}
	if err := syscall.Kill(-pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kunde inte stoppa testservern för %q: %w", alias, err)
	}
	return nil
}

func (s *TestserverStore) taBortRad(ctx context.Context, server *Testserver) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM pm_testservrar WHERE alias=? AND pid=?`, server.Alias, server.PID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pm_portar WHERE port=? AND projekt=? AND pid=?`, server.Port, server.Alias, server.PID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TestserverStore) underlag(ctx context.Context, alias string) (TestserverKonfig, string, error) {
	// Projektet kontrolleras först. Annars får den som skrivit fel alias veta
	// att startkommandot saknas, och letar efter fel sak.
	var repoPath string
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(repo_path,'') FROM projects WHERE alias=? AND archived_at IS NULL`, alias).Scan(&repoPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TestserverKonfig{}, "", fmt.Errorf("projektet %q finns inte i PM-workspacet. Lägg till det under Nytt projekt", alias)
		}
		return TestserverKonfig{}, "", err
	}
	serverKonfig, finns := s.konfig.Testserver[alias]
	if !finns {
		return TestserverKonfig{}, "", fmt.Errorf("projektet %q har inget startkommando. Lägg till ett testserverblock för %q under Konfig", alias, alias)
	}
	if serverKonfig.CWD == "" && repoPath == "" {
		return TestserverKonfig{}, "", fmt.Errorf("projektet %q saknar arbetskatalog för testservern. Sätt cwd i testserverblocket under Konfig", alias)
	}
	return serverKonfig, repoPath, nil
}

func ersattPort(args []string, port int) []string {
	ut := make([]string, len(args))
	for i, arg := range args {
		ut[i] = strings.ReplaceAll(arg, "{port}", strconv.Itoa(port))
	}
	return ut
}

func miljo(extra map[string]string) []string {
	ut := append([]string{}, os.Environ()...)
	for namn, varde := range extra {
		ut = append(ut, namn+"="+varde)
	}
	return ut
}

func processgruppLever(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(-pid, syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
