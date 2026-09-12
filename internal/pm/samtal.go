package pm

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/timeutil"
)

// Inlagg är ett inlägg i ett projektsamtal.
type Inlagg struct {
	ID            string       `json:"id"`
	ProjectID     string       `json:"project_id"`
	TaskID        string       `json:"task_id,omitempty"`
	Actor         models.Actor `json:"actor"`
	Text          string       `json:"text"`
	Minnesforslag string       `json:"minnesforslag,omitempty"`
	// KorningID pekar på körningen som skrev svaret, så agentens arbete går
	// att fälla ut även efter en omladdning.
	KorningID   string `json:"korning_id,omitempty"`
	KvitteradAt *int64 `json:"kvitterad_at,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

// SamtalStore läser och skriver projektsamtal i PM-databasen.
type SamtalStore struct {
	db *sql.DB
}

func NewSamtalStore(db *sql.DB) *SamtalStore { return &SamtalStore{db: db} }

// ProjectIDByAlias slår upp projektet och ger ett svenskt fel om det saknas.
func (s *SamtalStore) ProjectIDByAlias(ctx context.Context, alias string) (string, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", fmt.Errorf("ange projekt med -p <alias>")
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE alias = ?`, alias).Scan(&id)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("projektet %q finns inte i PM-workspacet", alias)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// Add sparar ett inlägg i projektets tråd.
func (s *SamtalStore) Add(ctx context.Context, projectID, taskID string, actor models.Actor, text string) (*Inlagg, error) {
	return s.AddMedMinnesforslag(ctx, projectID, taskID, actor, text, "")
}

// AddMedMinnesforslag sparar ett inlägg och agentens frivilliga minnesförslag.
func (s *SamtalStore) AddMedMinnesforslag(ctx context.Context, projectID, taskID string, actor models.Actor, text, minnesforslag string) (*Inlagg, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("inlägget saknar text")
	}
	if actor.Name == "" {
		return nil, fmt.Errorf("inlägget saknar aktör, ange --as human:namn eller ai:modell")
	}
	if actor.Kind != models.ActorKindHuman && actor.Kind != models.ActorKindAI {
		return nil, fmt.Errorf("aktören måste börja med human: eller ai:")
	}
	post := &Inlagg{
		ID:            ids.New(),
		ProjectID:     projectID,
		TaskID:        taskID,
		Actor:         actor,
		Text:          text,
		Minnesforslag: strings.TrimSpace(minnesforslag),
		CreatedAt:     timeutil.Now(),
	}
	var task any
	if taskID != "" {
		task = taskID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pm_samtal(id, project_id, task_id, actor_kind, actor_name, text, minnesforslag, korning_id, created_at)
		 VALUES(?,?,?,?,?,?,?,NULLIF(?,''),?)`,
		post.ID, post.ProjectID, task, string(post.Actor.Kind), post.Actor.Name, post.Text, post.Minnesforslag,
		post.KorningID, post.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("spara inlägg: %w", err)
	}
	return post, nil
}

// AddMedKorning sparar ett agentsvar och minns körningen som skrev det.
func (s *SamtalStore) AddMedKorning(ctx context.Context, projectID, korningID string, actor models.Actor, text, minnesforslag string) (*Inlagg, error) {
	post, err := s.AddMedMinnesforslag(ctx, projectID, "", actor, text, minnesforslag)
	if err != nil {
		return nil, err
	}
	if korningID == "" {
		return post, nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE pm_samtal SET korning_id = ? WHERE id = ?`, korningID, post.ID); err != nil {
		return nil, fmt.Errorf("koppla svaret till körningen: %w", err)
	}
	post.KorningID = korningID
	return post, nil
}

// List ger trådens inlägg i tidsordning. limit <= 0 ger alla.
func (s *SamtalStore) List(ctx context.Context, projectID string, limit int) ([]Inlagg, error) {
	query := `SELECT id, project_id, COALESCE(task_id,''), actor_kind, actor_name, text,
	                 minnesforslag, kvitterad_at, COALESCE(korning_id,''), created_at
	          FROM pm_samtal WHERE project_id = ? ORDER BY created_at ASC, id ASC`
	args := []any{projectID}
	if limit > 0 {
		// De senaste N, men fortfarande i stigande ordning i svaret.
		query = `SELECT id, project_id, task_id, actor_kind, actor_name, text, minnesforslag, kvitterad_at, korning_id, created_at FROM (
		           SELECT id, project_id, COALESCE(task_id,'') AS task_id, actor_kind, actor_name, text,
		                  minnesforslag, kvitterad_at, COALESCE(korning_id,'') AS korning_id, created_at
		           FROM pm_samtal WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT ?
		         ) ORDER BY created_at ASC, id ASC`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("läs samtal: %w", err)
	}
	defer rows.Close()

	poster := []Inlagg{}
	for rows.Next() {
		var p Inlagg
		var kind string
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.TaskID, &kind, &p.Actor.Name, &p.Text,
			&p.Minnesforslag, &p.KvitteradAt, &p.KorningID, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Actor.Kind = models.ActorKind(kind)
		poster = append(poster, p)
	}
	return poster, rows.Err()
}

// Kvittera markerar ett agentsvar som läst utan att skriva ett nytt inlägg.
func (s *SamtalStore) Kvittera(ctx context.Context, projectID, inlaggID string) error {
	resultat, err := s.db.ExecContext(ctx, `UPDATE pm_samtal SET kvitterad_at = ?
		WHERE id = ? AND project_id = ? AND actor_kind = 'ai'`, timeutil.Now(), inlaggID, projectID)
	if err != nil {
		return fmt.Errorf("kvittera agentsvar: %w", err)
	}
	antal, err := resultat.RowsAffected()
	if err != nil {
		return err
	}
	if antal == 0 {
		return fmt.Errorf("agentsvaret finns inte")
	}
	return nil
}

// Minnesforslag hämtar agentsvaret som får bli projektminne.
func (s *SamtalStore) Minnesforslag(ctx context.Context, projectID, inlaggID string) (*Inlagg, error) {
	var p Inlagg
	var kind string
	err := s.db.QueryRowContext(ctx, `SELECT id, project_id, COALESCE(task_id,''), actor_kind,
		actor_name, text, minnesforslag, kvitterad_at, created_at FROM pm_samtal
		WHERE id = ? AND project_id = ? AND actor_kind = 'ai'`, inlaggID, projectID).
		Scan(&p.ID, &p.ProjectID, &p.TaskID, &kind, &p.Actor.Name, &p.Text,
			&p.Minnesforslag, &p.KvitteradAt, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agentsvaret finns inte")
	}
	if err != nil {
		return nil, err
	}
	p.Actor.Kind = models.ActorKind(kind)
	if strings.TrimSpace(p.Minnesforslag) == "" {
		return nil, fmt.Errorf("agentsvaret saknar minnesförslag")
	}
	return &p, nil
}

// MarkeraMinnesforslagSparat tar bort förslaget när PM har sparat minnet.
func (s *SamtalStore) MarkeraMinnesforslagSparat(ctx context.Context, projectID, inlaggID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pm_samtal SET minnesforslag = '' WHERE id = ? AND project_id = ?`, inlaggID, projectID)
	return err
}

// ParseActor läser "human:rasmus" eller "ai:claude-opus-5".
func ParseActor(s string) (models.Actor, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return models.Actor{}, fmt.Errorf("aktören saknas")
	}
	kind, name, found := strings.Cut(s, ":")
	if !found || name == "" {
		return models.Actor{}, fmt.Errorf("skriv aktören %q som human:namn eller ai:modell", s)
	}
	switch models.ActorKind(kind) {
	case models.ActorKindHuman, models.ActorKindAI:
		return models.Actor{Kind: models.ActorKind(kind), Name: name}, nil
	}
	return models.Actor{}, fmt.Errorf("aktören %q måste börja med human: eller ai:", s)
}
