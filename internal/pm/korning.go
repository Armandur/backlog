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

	KorningSortTask    = "task"
	KorningSortFraga   = "fraga"
	KorningSortForslag = "forslag"
)

// Korning är en utdelad task.
type Korning struct {
	ID         string `json:"id"`
	Sort       string `json:"sort"`
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
	// Modell och Anstrangning är vad som begärdes, inte vad agenten svarade med.
	Modell       string `json:"modell,omitempty"`
	Anstrangning string `json:"anstrangning,omitempty"`
	SkapadAt     int64  `json:"skapad_at"`
	StartadAt    *int64 `json:"startad_at,omitempty"`
	SlutAt       *int64 `json:"slut_at,omitempty"`
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
	if k.Sort == "" {
		k.Sort = KorningSortTask
	}
	k.PID = os.Getpid()
	_, err := s.db.ExecContext(ctx,
		// NULLIF gör en tom task till NULL. En fråga i samtalet är en körning
		// utan task, och en tom sträng matchar ingen rad i tasks.
		`INSERT INTO pm_korningar(id, sort, project_id, task_id, task_ref, agent, motivering, status, repo_path, pid, logg_sokvag, modell, anstrangning, skapad_at)
		 VALUES(?,?,?,NULLIF(?,''),?,?,?,?,?,?,?,?,?,?)`,
		k.ID, k.Sort, k.ProjectID, k.TaskID, k.TaskRef, k.Agent, k.Motivering, k.Status, k.RepoPath, k.PID, k.Logg, k.Modell, k.Anstrangning, k.SkapadAt)
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

// SattLogg kopplar körningen till loggen innan agenten startar.
func (s *KorningStore) SattLogg(ctx context.Context, id, logg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pm_korningar SET logg_sokvag=? WHERE id=?`, logg, id)
	return err
}

// SattTokens sparar vad körningen kostade i nya tokens.
func (s *KorningStore) SattTokens(ctx context.Context, id string, tokens int) error {
	if tokens <= 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE pm_korningar SET tokens=? WHERE id=?`, tokens, id); err != nil {
		return fmt.Errorf("spara körningens tokens: %w", err)
	}
	return nil
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
	return s.ListaFiltrerad(ctx, KorningFilter{
		ProjectID: projectID, Sorter: []string{KorningSortTask, KorningSortFraga}, Limit: limit,
	})
}

// KorningFilter avgränsar körningar för webbens statusfilter och cursor.
type KorningFilter struct {
	ProjectID string
	Status    string
	Sorter    []string
	Innan     int64
	Limit     int
}

// ListaFiltrerad ger körningar i fallande tidsordning.
func (s *KorningStore) ListaFiltrerad(ctx context.Context, filter KorningFilter) ([]Korning, error) {
	q := `SELECT ` + kolumner + ` FROM pm_korningar`
	villkor := []string{}
	args := []any{}
	if filter.ProjectID != "" {
		villkor = append(villkor, `project_id = ?`)
		args = append(args, filter.ProjectID)
	}
	if len(filter.Sorter) > 0 {
		platshallare := make([]string, len(filter.Sorter))
		for i, sort := range filter.Sorter {
			platshallare[i] = "?"
			args = append(args, sort)
		}
		villkor = append(villkor, `sort IN (`+strings.Join(platshallare, ",")+`)`)
	}
	switch filter.Status {
	case "pagaende":
		villkor = append(villkor, `status IN (?,?)`)
		args = append(args, StatusKoad, StatusKor)
	case "avslutade":
		villkor = append(villkor, `status IN (?,?)`)
		args = append(args, StatusKlar, StatusFel)
	case StatusKlar, StatusFel:
		villkor = append(villkor, `status = ?`)
		args = append(args, filter.Status)
	}
	if filter.Innan > 0 {
		villkor = append(villkor, `skapad_at < ?`)
		args = append(args, filter.Innan)
	}
	if len(villkor) > 0 {
		q += ` WHERE ` + strings.Join(villkor, ` AND `)
	}
	q += ` ORDER BY skapad_at DESC`
	if filter.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, filter.Limit)
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
		SkrivAvbrottshandelse(k.Logg)
		antal++
	}
	return antal, nil
}

// StadaOmOvergiven markerar EN körning som fel om processen som startade den
// är borta. Strömvyn frågar per anrop, så en körning som dog med servern inte
// ser ut att pågå för evigt. Andra värdet säger om städningen gjorde det, så
// en körning som misslyckades på egen hand inte beskrivs som avbruten.
func (s *KorningStore) StadaOmOvergiven(ctx context.Context, id string) (*Korning, bool, error) {
	k, err := s.Hamta(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if k.Status != StatusKoad && k.Status != StatusKor {
		return k, false, nil
	}
	if k.PID == 0 || k.PID == os.Getpid() || processLever(k.PID) {
		return k, false, nil
	}
	if err := s.Avsluta(ctx, k.ID, StatusFel, 1, k.Logg); err != nil {
		return k, false, err
	}
	SkrivAvbrottshandelse(k.Logg)
	uppdaterad, err := s.Hamta(ctx, id)
	if err != nil {
		return k, true, err
	}
	return uppdaterad, true, nil
}

const kolumner = `id, sort, project_id, task_id, task_ref, agent, motivering, status, repo_path, pid, exit_kod, logg_sokvag, modell, anstrangning, skapad_at, startad_at, slut_at`

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
		// En fråga i samtalet är en körning utan task, och då är task_id NULL.
		var taskID sql.NullString
		if err := rows.Scan(&k.ID, &k.Sort, &k.ProjectID, &taskID, &k.TaskRef, &k.Agent, &k.Motivering, &k.Status,
			&k.RepoPath, &k.PID, &exit, &k.Logg, &k.Modell, &k.Anstrangning, &k.SkapadAt, &startad, &slut); err != nil {
			return nil, err
		}
		k.TaskID = taskID.String
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
