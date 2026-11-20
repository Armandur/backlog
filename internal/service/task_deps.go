package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/timeutil"
)

// applyDependencies adds and removes depends-on edges for task t. All refs
// are resolved and every edge is checked before anything is written, and the
// writes run in one transaction, so a rejected edge leaves no partial change.
func (s *TaskService) applyDependencies(ctx context.Context, t *models.Task, add, remove []string, actor models.Actor) error {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	addTasks, err := s.resolveDependencyRefs(ctx, t, add)
	if err != nil {
		return err
	}
	removeTasks, err := s.resolveDependencyRefs(ctx, t, remove)
	if err != nil {
		return err
	}

	tx, err := s.deps.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	now := timeutil.Now()
	var added, removed []string
	for _, dep := range removeTasks {
		ok, err := s.deps.Remove(ctx, tx, t.ID, dep.ID)
		if err != nil {
			return fmt.Errorf("remove dependency: %w", err)
		}
		if !ok {
			return fmt.Errorf("TASK-%d does not depend on TASK-%d", t.Seq, dep.Seq)
		}
		removed = append(removed, fmt.Sprintf("TASK-%d", dep.Seq))
	}
	for _, dep := range addTasks {
		// A cycle appears if t is already reachable from dep.
		path, err := s.deps.Path(ctx, tx, dep.ID, t.ID)
		if err != nil {
			return fmt.Errorf("check dependency cycle: %w", err)
		}
		if path != nil {
			// Release the only pool connection before cycleText reads tasks.
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("TASK-%d cannot depend on TASK-%d: that would create a cycle (%s)",
				t.Seq, dep.Seq, s.cycleText(ctx, t.Seq, path))
		}
		ok, err := s.deps.Add(ctx, tx, t.ID, dep.ID, now)
		if err != nil {
			return fmt.Errorf("add dependency: %w", err)
		}
		if ok {
			added = append(added, fmt.Sprintf("TASK-%d", dep.Seq))
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if len(added) > 0 {
		s.logActivity(ctx, t.ProjectID, "task", t.ID, "dependency_added",
			fmt.Sprintf("TASK-%d now depends on %s", t.Seq, strings.Join(added, ", ")), actor)
	}
	if len(removed) > 0 {
		s.logActivity(ctx, t.ProjectID, "task", t.ID, "dependency_removed",
			fmt.Sprintf("TASK-%d no longer depends on %s", t.Seq, strings.Join(removed, ", ")), actor)
	}
	return nil
}

func (s *TaskService) resolveDependencyRefs(ctx context.Context, t *models.Task, refs []string) ([]*models.Task, error) {
	var out []*models.Task
	for _, ref := range refs {
		id, err := s.resolveID(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("dependency %s: %w", ref, err)
		}
		dep, err := s.tasks.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("dependency %s: %w", ref, err)
		}
		if dep.ID == t.ID {
			return nil, fmt.Errorf("TASK-%d cannot depend on itself", t.Seq)
		}
		out = append(out, dep)
	}
	return out, nil
}

// cycleText renders the would-be cycle as "TASK-1 -> TASK-2 -> TASK-1".
// path runs from the new dependency to the task itself.
func (s *TaskService) cycleText(ctx context.Context, taskSeq int, path []string) string {
	parts := []string{fmt.Sprintf("TASK-%d", taskSeq)}
	for _, id := range path {
		if t, err := s.tasks.GetByID(ctx, id); err == nil {
			parts = append(parts, fmt.Sprintf("TASK-%d", t.Seq))
		} else {
			parts = append(parts, id)
		}
	}
	return strings.Join(parts, " -> ")
}

// loadDependencies fills DependsOn and Blocks on t.
func (s *TaskService) loadDependencies(ctx context.Context, t *models.Task) error {
	dependsOn, err := s.deps.DependsOn(ctx, t.ID)
	if err != nil {
		return fmt.Errorf("load dependencies for task %s: %w", t.ID, err)
	}
	blocks, err := s.deps.Blocks(ctx, t.ID)
	if err != nil {
		return fmt.Errorf("load dependents for task %s: %w", t.ID, err)
	}
	t.DependsOn = dependsOn
	t.Blocks = blocks
	return nil
}
