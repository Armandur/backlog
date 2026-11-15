package service

import (
	"context"
	"fmt"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/timeutil"
)

// transferTarget resolves the project a task or doc is being moved to.
func transferTarget(ctx context.Context, projects *repo.ProjectRepo, alias, currentID string) (*models.Project, error) {
	target, err := projects.GetByAlias(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("project %q: %w", alias, err)
	}
	if target.ArchivedAt != nil {
		return nil, fmt.Errorf("project %q is archived", alias)
	}
	if target.ID == currentID {
		return nil, fmt.Errorf("already in project %q", alias)
	}
	return target, nil
}

// Transfer moves a task to another project. The TASK-N reference, plans,
// comments and attachments follow the task. Labels are recreated by name in
// the target project.
func (s *TaskService) Transfer(ctx context.Context, ref, targetAlias string, actor models.Actor) (*models.Task, error) {
	id, err := s.resolveID(ctx, ref)
	if err != nil {
		return nil, err
	}
	t, err := s.tasks.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	target, err := transferTarget(ctx, s.projects, targetAlias, t.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("TASK-%d: %w", t.Seq, err)
	}
	source, err := s.projects.GetByID(ctx, t.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := s.tasks.Transfer(ctx, t.ID, target.ID, timeutil.Now()); err != nil {
		return nil, fmt.Errorf("transfer task: %w", err)
	}
	summary := fmt.Sprintf("Moved task TASK-%d %q from %s to %s", t.Seq, t.Title, source.Alias, target.Alias)
	s.logActivity(ctx, source.ID, "task", t.ID, "transferred", summary, actor)
	s.logActivity(ctx, target.ID, "task", t.ID, "transferred", summary, actor)
	return s.Get(ctx, t.ID, false, false)
}

// Transfer moves a doc, with its version history, to another project.
func (s *DocService) Transfer(ctx context.Context, id, targetAlias string, actor models.Actor) (*models.Doc, error) {
	id, err := s.docs.ResolveID(ctx, id)
	if err != nil {
		return nil, err
	}
	d, err := s.docs.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("doc not found: %w", err)
	}
	target, err := transferTarget(ctx, s.projects, targetAlias, d.ProjectID)
	if err != nil {
		return nil, err
	}
	source, err := s.projects.GetByID(ctx, d.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := s.docs.SetProject(ctx, d.ID, target.ID, timeutil.Now()); err != nil {
		return nil, fmt.Errorf("transfer doc: %w", err)
	}
	summary := fmt.Sprintf("Moved doc %q from %s to %s", d.Title, source.Alias, target.Alias)
	s.logActivity(ctx, source.ID, "doc", d.ID, "transferred", summary, actor)
	s.logActivity(ctx, target.ID, "doc", d.ID, "transferred", summary, actor)
	return s.Get(ctx, d.ID)
}

// Transfer moves a memory entry to another project.
func (s *MemoryService) Transfer(ctx context.Context, id, targetAlias string, actor models.Actor) (*models.Memory, error) {
	id, err := s.memory.ResolveID(ctx, id)
	if err != nil {
		return nil, err
	}
	m, err := s.memory.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("memory entry not found: %w", err)
	}
	target, err := transferTarget(ctx, s.projects, targetAlias, m.ProjectID)
	if err != nil {
		return nil, err
	}
	source, err := s.projects.GetByID(ctx, m.ProjectID)
	if err != nil {
		return nil, err
	}
	if err := s.memory.SetProject(ctx, m.ID, target.ID); err != nil {
		return nil, fmt.Errorf("transfer memory: %w", err)
	}
	summary := fmt.Sprintf("Moved memory entry from %s to %s", source.Alias, target.Alias)
	s.logActivity(ctx, source.ID, "memory", m.ID, "transferred", summary, actor)
	s.logActivity(ctx, target.ID, "memory", m.ID, "transferred", summary, actor)
	m.ProjectID = target.ID
	return m, nil
}
