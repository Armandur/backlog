package repo

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mazen160/backlog/internal/ids"
)

// Transfer moves a task to another project in one transaction. Labels are
// scoped to a project, so every label on the task is re-pointed to the label
// with the same name in the target project, which is created if missing.
func (r *TaskRepo) Transfer(ctx context.Context, taskID, targetProjectID string, updatedAt int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type label struct{ id, name, color string }
	rows, err := tx.QueryContext(ctx,
		`SELECT l.id,l.name,l.color FROM labels l
		 JOIN task_labels tl ON tl.label_id=l.id WHERE tl.task_id=?`, taskID)
	if err != nil {
		return fmt.Errorf("list labels: %w", err)
	}
	var labels []label
	for rows.Next() {
		var l label
		if err := rows.Scan(&l.id, &l.name, &l.color); err != nil {
			rows.Close()
			return err
		}
		labels = append(labels, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, l := range labels {
		var targetID string
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM labels WHERE project_id=? AND name=?`, targetProjectID, l.name).Scan(&targetID)
		if err == sql.ErrNoRows {
			targetID = ids.New()
			_, err = tx.ExecContext(ctx,
				`INSERT INTO labels(id,project_id,name,color) VALUES(?,?,?,?)`,
				targetID, targetProjectID, l.name, l.color)
		}
		if err != nil {
			return fmt.Errorf("label %q: %w", l.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM task_labels WHERE task_id=? AND label_id=?`, taskID, l.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO task_labels(task_id,label_id) VALUES(?,?)`, taskID, targetID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET project_id=?,updated_at=? WHERE id=?`, targetProjectID, updatedAt, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetProject moves a doc, with all its versions, to another project.
func (r *DocRepo) SetProject(ctx context.Context, id, projectID string, updatedAt int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE project_docs SET project_id=?,updated_at=? WHERE id=?`, projectID, updatedAt, id)
	return err
}

// SetProject moves a memory entry to another project. The entry keeps its ID,
// author and timestamp.
func (r *MemoryRepo) SetProject(ctx context.Context, id, projectID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE project_memory SET project_id=? WHERE id=?`, projectID, id)
	return err
}
