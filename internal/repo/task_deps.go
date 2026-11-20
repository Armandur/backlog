package repo

import (
	"context"
	"database/sql"
	"strings"

	"github.com/mazen160/backlog/internal/models"
)

// DependencyRepo reads and writes task_dependencies. A row (task, dep) means
// task waits for dep.
type DependencyRepo struct{ db *sql.DB }

func NewDependencyRepo(db *sql.DB) *DependencyRepo { return &DependencyRepo{db: db} }

// Begin starts a transaction. The pool holds one connection, so every query
// inside the transaction must go through the returned *sql.Tx.
func (r *DependencyRepo) Begin(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}

// Add inserts the edge. It reports false when the edge already existed.
func (r *DependencyRepo) Add(ctx context.Context, runner Runner, taskID, dependsOnID string, now int64) (bool, error) {
	res, err := runner.ExecContext(ctx,
		`INSERT OR IGNORE INTO task_dependencies(task_id, depends_on_id, created_at) VALUES(?,?,?)`,
		taskID, dependsOnID, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Remove deletes the edge. It reports false when there was no such edge.
func (r *DependencyRepo) Remove(ctx context.Context, runner Runner, taskID, dependsOnID string) (bool, error) {
	res, err := runner.ExecContext(ctx,
		`DELETE FROM task_dependencies WHERE task_id=? AND depends_on_id=?`, taskID, dependsOnID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Path returns the chain of task IDs from fromID to toID along existing
// depends-on edges, both ends included, or nil when toID is not reachable.
// Adding the edge toID -> fromID would close exactly this chain into a cycle.
func (r *DependencyRepo) Path(ctx context.Context, runner Runner, fromID, toID string) ([]string, error) {
	var path string
	err := runner.QueryRowContext(ctx, `
		WITH RECURSIVE reach(id, path) AS (
			SELECT ?, ?
			UNION
			SELECT d.depends_on_id, reach.path || ',' || d.depends_on_id
			FROM task_dependencies d JOIN reach ON d.task_id = reach.id
			WHERE instr(reach.path, d.depends_on_id) = 0
		)
		SELECT path FROM reach WHERE id = ? LIMIT 1`,
		fromID, fromID, toID).Scan(&path)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(path, ","), nil
}

// DependsOn lists the tasks taskID waits for.
func (r *DependencyRepo) DependsOn(ctx context.Context, taskID string) ([]models.TaskRef, error) {
	return r.refs(ctx, `SELECT t.id, COALESCE(t.task_seq,0), t.title, t.status
		FROM task_dependencies d JOIN tasks t ON t.id = d.depends_on_id
		WHERE d.task_id = ? ORDER BY t.task_seq`, taskID)
}

// Blocks lists the tasks that wait for taskID.
func (r *DependencyRepo) Blocks(ctx context.Context, taskID string) ([]models.TaskRef, error) {
	return r.refs(ctx, `SELECT t.id, COALESCE(t.task_seq,0), t.title, t.status
		FROM task_dependencies d JOIN tasks t ON t.id = d.task_id
		WHERE d.depends_on_id = ? ORDER BY t.task_seq`, taskID)
}

func (r *DependencyRepo) refs(ctx context.Context, q, taskID string) ([]models.TaskRef, error) {
	rows, err := r.db.QueryContext(ctx, q, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.TaskRef{}
	for rows.Next() {
		var ref models.TaskRef
		if err := rows.Scan(&ref.ID, &ref.Seq, &ref.Title, &ref.Status); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}
