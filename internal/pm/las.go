package pm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RepoLas är PM:s eget lås per repo-sökväg. Två körningar mot samma katalog
// kör aldrig samtidigt, oberoende av externa verktyg som arbetar.
type RepoLas struct {
	sokvag string
	tagen  bool
}

type lasInnehall struct {
	PID      int    `json:"pid"`
	Starttid string `json:"starttid"`
	Korning  string `json:"korning"`
	Repo     string `json:"repo"`
	Sedan    string `json:"sedan"`
}

// LasKatalog är katalogen med låsfiler i PM-profilen.
func LasKatalog(workspaceDir string) string { return filepath.Join(workspaceDir, "las") }

// NyRepoLas pekar ut låsfilen för en repo-sökväg.
func NyRepoLas(workspaceDir, repoPath string) *RepoLas {
	summa := sha256.Sum256([]byte(filepath.Clean(repoPath)))
	namn := hex.EncodeToString(summa[:8]) + ".lock"
	return &RepoLas{sokvag: filepath.Join(LasKatalog(workspaceDir), namn)}
}

// Sokvag ger låsfilens sökväg.
func (l *RepoLas) Sokvag() string { return l.sokvag }

// Ta försöker ta låset. Andra värdet är sant när låset togs, annars är det
// upptaget av en levande process.
func (l *RepoLas) Ta(repo, korning string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(l.sokvag), 0o755); err != nil {
		return false, err
	}
	if agare, lever, err := l.Agare(); err != nil {
		return false, err
	} else if lever {
		return false, nil
	} else if agare != nil {
		// Inaktuellt lås efter en död process.
		os.Remove(l.sokvag)
	}
	starttid, err := processStarttid(os.Getpid())
	if err != nil {
		return false, fmt.Errorf("kunde inte läsa processens starttid: %w", err)
	}

	f, err := os.OpenFile(l.sokvag, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	data, _ := json.Marshal(lasInnehall{
		PID:      os.Getpid(),
		Starttid: starttid,
		Korning:  korning,
		Repo:     repo,
		Sedan:    time.Now().Format(time.RFC3339),
	})
	if _, err := f.Write(data); err != nil {
		return false, err
	}
	l.tagen = true
	return true, nil
}

// Agare läser låsfilen. lever är sant när processen som håller låset finns kvar.
func (l *RepoLas) Agare() (*lasInnehall, bool, error) {
	data, err := os.ReadFile(l.sokvag)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var innehall lasInnehall
	if err := json.Unmarshal(data, &innehall); err != nil {
		// En trasig låsfil är inaktuell.
		return &lasInnehall{}, false, nil
	}
	return &innehall, processMatcharStarttid(innehall.PID, innehall.Starttid), nil
}

// Slapp släpper låset om vi håller det.
func (l *RepoLas) Slapp() error {
	if !l.tagen {
		return nil
	}
	l.tagen = false
	if err := os.Remove(l.sokvag); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// VantaOchTa väntar tills låset går att ta, eller ger upp efter timeout.
// Väntaren äger starten: körningen står som köad tills låset är taget.
func (l *RepoLas) VantaOchTa(repo, korning string, timeout, intervall time.Duration) (bool, error) {
	slut := time.Now().Add(timeout)
	for {
		tagen, err := l.Ta(repo, korning)
		if err != nil || tagen {
			return tagen, err
		}
		if time.Now().After(slut) {
			return false, nil
		}
		time.Sleep(intervall)
	}
}

func processLever(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func processMatcharStarttid(pid int, starttid string) bool {
	if starttid == "" || !processLever(pid) {
		return false
	}
	faktiskStarttid, err := processStarttid(pid)
	if err != nil {
		// Går ps inte att fråga vet PM ingenting. Då är det säkrare att låta
		// låset stå kvar än att två körningar tar samma repo.
		var exitFel *exec.ExitError
		if errors.As(err, &exitFel) && exitFel.ExitCode() == 1 {
			return false
		}
		fmt.Fprintf(os.Stderr, "kunde inte läsa starttiden för process %d: %v\n", pid, err)
		return true
	}
	return faktiskStarttid == starttid
}

// processStarttid använder ps eftersom kommandot finns på både Linux och macOS.
func processStarttid(pid int) (string, error) {
	utdata, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	starttid := strings.TrimSpace(string(utdata))
	if starttid == "" {
		return "", fmt.Errorf("process %d saknar starttid", pid)
	}
	return starttid, nil
}

// LasBesked beskriver vem som håller låset, för besked till Rasmus.
func (l *RepoLas) LasBesked() string {
	agare, lever, err := l.Agare()
	if err != nil || agare == nil {
		return ""
	}
	if !lever {
		return fmt.Sprintf("inaktuellt lås från pid %d", agare.PID)
	}
	return fmt.Sprintf("repot används av körning %s (pid %d) sedan %s", agare.Korning, agare.PID, agare.Sedan)
}
