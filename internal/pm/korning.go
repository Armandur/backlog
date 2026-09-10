package pm

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/timeutil"
)

// Körningsstatus.
const (
	StatusKoad = "koad"
	StatusKor  = "kor"
	StatusKlar = "klar"
	StatusFel  = "fel"
)

// Korning är en utdelad task.
type Korning struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	TaskID     string `json:"task_id"`
	TaskRef    string `json:"task_ref"`
	Agent      string `json:"agent"`
	Motivering string `json:"motivering"`
	Status     string `json:"status"`
	RepoPath   string `json:"repo_path"`
	PID        int    `json:"pid"`
	ExitKod    *int   `json:"exit_kod,omitempty"`
	Logg       string `json:"logg_sokvag,omitempty"`
	SkapadAt   int64  `json:"skapad_at"`
	StartadAt  *int64 `json:"startad_at,omitempty"`
	SlutAt     *int64 `json:"slut_at,omitempty"`
}

// KorningStore läser och skriver körningar.
type KorningStore struct{ db *sql.DB }

func NewKorningStore(db *sql.DB) *KorningStore { return &KorningStore{db: db} }

func (s *KorningStore) Skapa(ctx context.Context, k *Korning) error {
	k.ID = ids.New()
	k.SkapadAt = timeutil.Now()
	if k.Status == "" {
		k.Status = StatusKoad
	}
	k.PID = os.Getpid()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pm_korningar(id, project_id, task_id, task_ref, agent, motivering, status, repo_path, pid, logg_sokvag, skapad_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		k.ID, k.ProjectID, k.TaskID, k.TaskRef, k.Agent, k.Motivering, k.Status, k.RepoPath, k.PID, k.Logg, k.SkapadAt)
	if err != nil {
		return fmt.Errorf("skapa körning: %w", err)
	}
	return nil
}

func (s *KorningStore) SattStatus(ctx context.Context, id, status string) error {
	nu := timeutil.Now()
	var err error
	switch status {
	case StatusKor:
		_, err = s.db.ExecContext(ctx, `UPDATE pm_korningar SET status=?, startad_at=? WHERE id=?`, status, nu, id)
	default:
		_, err = s.db.ExecContext(ctx, `UPDATE pm_korningar SET status=? WHERE id=?`, status, id)
	}
	return err
}

func (s *KorningStore) Avsluta(ctx context.Context, id, status string, exitKod int, logg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pm_korningar SET status=?, exit_kod=?, logg_sokvag=?, slut_at=? WHERE id=?`,
		status, exitKod, logg, timeutil.Now(), id)
	return err
}

func (s *KorningStore) Hamta(ctx context.Context, id string) (*Korning, error) {
	rader, err := s.fraga(ctx, `SELECT `+kolumner+` FROM pm_korningar WHERE id = ? OR id LIKE ?`, id, id+"%")
	if err != nil {
		return nil, err
	}
	if len(rader) == 0 {
		return nil, fmt.Errorf("körningen %q finns inte", id)
	}
	return &rader[0], nil
}

func (s *KorningStore) Lista(ctx context.Context, projectID string, limit int) ([]Korning, error) {
	q := `SELECT ` + kolumner + ` FROM pm_korningar`
	args := []any{}
	if projectID != "" {
		q += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	q += ` ORDER BY skapad_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	return s.fraga(ctx, q, args...)
}

// Koade ger köade körningar för en repo-sökväg, äldst först.
func (s *KorningStore) Koade(ctx context.Context, repoPath string) ([]Korning, error) {
	return s.fraga(ctx, `SELECT `+kolumner+` FROM pm_korningar WHERE status=? AND repo_path=? ORDER BY skapad_at ASC`,
		StatusKoad, repoPath)
}

// StadaOvergivna markerar körningar vars process är borta som fel, så en
// dödad väntare inte lämnar en körning i koad för alltid.
func (s *KorningStore) StadaOvergivna(ctx context.Context) (int, error) {
	rader, err := s.fraga(ctx, `SELECT `+kolumner+` FROM pm_korningar WHERE status IN (?,?)`, StatusKoad, StatusKor)
	if err != nil {
		return 0, err
	}
	antal := 0
	for _, k := range rader {
		if k.PID == 0 || k.PID == os.Getpid() || processLever(k.PID) {
			continue
		}
		if err := s.Avsluta(ctx, k.ID, StatusFel, 1, k.Logg); err != nil {
			return antal, err
		}
		antal++
	}
	return antal, nil
}

const kolumner = `id, project_id, task_id, task_ref, agent, motivering, status, repo_path, pid, exit_kod, logg_sokvag, skapad_at, startad_at, slut_at`

func (s *KorningStore) fraga(ctx context.Context, q string, args ...any) ([]Korning, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("läs körningar: %w", err)
	}
	defer rows.Close()
	ut := []Korning{}
	for rows.Next() {
		var k Korning
		var exit sql.NullInt64
		var startad, slut sql.NullInt64
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.TaskID, &k.TaskRef, &k.Agent, &k.Motivering, &k.Status,
			&k.RepoPath, &k.PID, &exit, &k.Logg, &k.SkapadAt, &startad, &slut); err != nil {
			return nil, err
		}
		if exit.Valid {
			v := int(exit.Int64)
			k.ExitKod = &v
		}
		if startad.Valid {
			v := startad.Int64
			k.StartadAt = &v
		}
		if slut.Valid {
			v := slut.Int64
			k.SlutAt = &v
		}
		ut = append(ut, k)
	}
	return ut, rows.Err()
}

// LasLogg ger loggen för en körning.
func LasLogg(sokvag string) string {
	if sokvag == "" {
		return ""
	}
	data, err := os.ReadFile(sokvag)
	if err != nil {
		return fmt.Sprintf("(loggen kunde inte läsas: %v)", err)
	}
	return strings.TrimRight(string(data), "\n")
}
