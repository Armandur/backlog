package pm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mazen160/backlog/internal/timeutil"
)

const testserverStopptid = 10 * time.Second

// Testserver beskriver en process som PM har startat och därför får stoppa.
type Testserver struct {
	Alias     string `json:"alias"`
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartadAt int64  `json:"startad_at"`
	Logg      string `json:"logg_sokvag"`
	Lever     bool   `json:"lever"`
}

// TestserverStore äger testserverprocesser och deras databasrader.
type TestserverStore struct {
	db        *sql.DB
	konfig    Konfig
	workspace string
	stopptid  time.Duration
}

func NewTestserverStore(db *sql.DB, konfig Konfig, workspace string) *TestserverStore {
	return &TestserverStore{db: db, konfig: konfig, workspace: workspace, stopptid: testserverStopptid}
}

// Starta reserverar porten, startar en processgrupp och sparar dess ägarskap.
func (s *TestserverStore) Starta(ctx context.Context, alias string) (*Testserver, error) {
	serverKonfig, repoPath, err := s.underlag(ctx, alias)
	if err != nil {
		return nil, err
	}
	if befintlig, err := s.Hamta(ctx, alias); err == nil {
		if befintlig.Lever {
			return nil, fmt.Errorf("testservern för %q kör redan på port %d", alias, befintlig.Port)
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

	logg := filepath.Join(s.workspace, "loggar", "testserver-"+alias+".log")
	if err := os.MkdirAll(filepath.Dir(logg), 0o755); err != nil {
		return nil, fmt.Errorf("kunde inte skapa testserverns loggkatalog: %w", err)
	}
	loggfil, err := os.OpenFile(logg, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("kunde inte öppna testserverns logg: %w", err)
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
		return nil, fmt.Errorf("testservern för %q startades redan av en annan process", alias)
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
		return nil, fmt.Errorf("kunde inte starta testservern för %q: %w", alias, err)
	}
	pid := kommando.Process.Pid
	go func() { _ = kommando.Wait() }()
	if err := s.kopplaProcess(ctx, alias, reservation.Port, pid); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return nil, err
	}

	reserverad = false
	radSparad = false
	return &Testserver{Alias: alias, PID: pid, Port: reservation.Port, StartadAt: nu, Logg: logg, Lever: true}, nil
}

// Hamta läser status utan att ändra raden.
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
		return nil, fmt.Errorf("testservern för %q kör inte", alias)
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
				return nil, fmt.Errorf("testservern för %q kunde inte stoppas", alias)
			}
		}
	}
	if err := s.taBortRad(ctx, server); err != nil {
		return nil, err
	}
	server.Lever = false
	return server, nil
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
		return fmt.Errorf("testserverns startrad försvann innan processen kunde sparas")
	}
	reservation, err := tx.ExecContext(ctx,
		`UPDATE pm_portar SET pid=? WHERE port=? AND projekt=? AND pid=?`, pid, port, alias, os.Getpid())
	if err != nil {
		return fmt.Errorf("kunde inte koppla porten till testservern: %w", err)
	}
	antal, err = reservation.RowsAffected()
	if err != nil || antal != 1 {
		return fmt.Errorf("testserverns portreservation försvann innan processen kunde sparas")
	}
	return tx.Commit()
}

func (s *TestserverStore) signaleraRegistrerad(ctx context.Context, alias string, pid int, signal syscall.Signal) error {
	var registreradPID int
	if err := s.db.QueryRowContext(ctx, `SELECT pid FROM pm_testservrar WHERE alias=?`, alias).Scan(&registreradPID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("testservern saknar en registrerad process och stoppades inte")
		}
		return err
	}
	if registreradPID != pid {
		return fmt.Errorf("testserverns registrerade process ändrades och stoppades inte")
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
	serverKonfig, finns := s.konfig.Testserver[alias]
	if !finns {
		return TestserverKonfig{}, "", fmt.Errorf("projektet %q saknar konfiguration för testserver", alias)
	}
	var repoPath string
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(repo_path,'') FROM projects WHERE alias=? AND archived_at IS NULL`, alias).Scan(&repoPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TestserverKonfig{}, "", fmt.Errorf("projektet %q finns inte i PM-workspacet", alias)
		}
		return TestserverKonfig{}, "", err
	}
	if serverKonfig.CWD == "" && repoPath == "" {
		return TestserverKonfig{}, "", fmt.Errorf("projektet %q saknar arbetskatalog för testservern", alias)
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
