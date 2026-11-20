package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mazen160/backlog/internal/ids"
	"github.com/mazen160/backlog/internal/models"
	extrepo "github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/timeutil"
)

// ImportResult summarises a cross-workspace import.
type ImportResult struct {
	Projects     int
	Tasks        int
	Plans        int
	Comments     int
	Labels       int
	Dependencies int
	// SkippedDependencies counts imported tasks' dependencies on tasks that
	// were left out of the import, e.g. in another project under --project.
	SkippedDependencies int
}

// ImportFromDB copies tasks (and their plans/comments) from srcDB into dstDB.
// If projectAlias is non-empty, only tasks in that project are imported.
func ImportFromDB(ctx context.Context, dstDB, srcDB *sql.DB, projectAlias string, actor models.Actor, dryRun bool) (*ImportResult, error) {
	srcTasks := extrepo.NewTaskRepo(srcDB)
	srcPlans := extrepo.NewPlanRepo(srcDB)
	srcComments := extrepo.NewCommentRepo(srcDB)
	srcLabels := extrepo.NewLabelRepo(srcDB)
	srcProjects := extrepo.NewProjectRepo(srcDB)

	dstProjects := extrepo.NewProjectRepo(dstDB)
	dstTasks := extrepo.NewTaskRepo(dstDB)
	dstPlans := extrepo.NewPlanRepo(dstDB)
	dstComments := extrepo.NewCommentRepo(dstDB)
	dstDeps := extrepo.NewDependencyRepo(dstDB)
	dstLabelSvc := NewLabelService(dstDB)

	result := &ImportResult{}

	// Load source projects
	srcProjList, err := srcProjects.List(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("import: list source projects: %w", err)
	}

	// project ID remapping: srcID -> dstID
	projMap := map[string]string{}

	for _, sp := range srcProjList {
		if projectAlias != "" && sp.Alias != projectAlias {
			continue
		}
		// Find or create project in dst
		dp, _ := dstProjects.GetByAlias(ctx, sp.Alias)
		var dstProjectID string
		if dp != nil {
			dstProjectID = dp.ID
		} else {
			newID := ids.New()
			now := timeutil.Now()
			np := &models.Project{
				ID:          newID,
				Alias:       sp.Alias,
				Name:        sp.Name,
				Description: sp.Description,
				RepoPath:    sp.RepoPath,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if !dryRun {
				if err := dstProjects.Insert(ctx, np); err != nil {
					return nil, fmt.Errorf("import: create project %q: %w", sp.Alias, err)
				}
			}
			dstProjectID = newID
			result.Projects++
		}
		projMap[sp.ID] = dstProjectID

		// Labels
		srcLabelList, _ := srcLabels.ListForProject(ctx, sp.ID)
		for _, sl := range srcLabelList {
			if !dryRun {
				dstLabelSvc.Create(ctx, models.CreateLabelInput{ //nolint:errcheck
					ProjectID: dstProjectID,
					Name:      sl.Name,
					Color:     sl.Color,
				})
			}
			result.Labels++
		}
	}

	// Tasks
	allTasks, err := srcTasks.List(ctx, models.TaskFilter{IncludeArchived: true})
	if err != nil {
		return nil, fmt.Errorf("import: list source tasks: %w", err)
	}

	// task ID remapping: srcID -> dstID, for dependencies
	taskMap := map[string]string{}

	for _, st := range allTasks {
		dstProjID, ok := projMap[st.ProjectID]
		if !ok {
			continue
		}
		newID := ids.New()
		taskMap[st.ID] = newID
		now := timeutil.Now()
		extRef := fmt.Sprintf("imported:%s", st.ID)
		if st.ExternalRef != "" {
			extRef = st.ExternalRef
		}
		nt := &models.Task{
			ID:          newID,
			ProjectID:   dstProjID,
			Title:       st.Title,
			Description: st.Description,
			Type:        st.Type,
			Status:      st.Status,
			Priority:    st.Priority,
			Assignee:    st.Assignee,
			DueAt:       st.DueAt,
			Actor:       actor,
			Source:      st.Source,
			ExternalRef: extRef,
			CompletedAt: st.CompletedAt,
			ArchivedAt:  st.ArchivedAt,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if !dryRun {
			if err := dstTasks.Insert(ctx, nt); err != nil {
				return nil, fmt.Errorf("import: insert task: %w", err)
			}
		}
		result.Tasks++

		// Labels for task
		srcTaskLabels, _ := srcLabels.ListForTask(ctx, st.ID)
		for _, sl := range srcTaskLabels {
			if !dryRun {
				dstLabelSvc.AttachByName(ctx, dstProjID, newID, sl.Name) //nolint:errcheck
			}
		}

		// Plans
		srcPlanList, _ := srcPlans.ListPlansForTask(ctx, st.ID, true)
		for _, sp := range srcPlanList {
			newPlanID := ids.New()
			np := &models.Plan{
				ID:             newPlanID,
				TaskID:         newID,
				CurrentVersion: sp.CurrentVersion,
				Source:         sp.Source,
				ArchivedAt:     sp.ArchivedAt,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if !dryRun {
				if err := dstPlans.InsertPlan(ctx, np); err != nil {
					return nil, fmt.Errorf("import: insert plan: %w", err)
				}
			}
			result.Plans++

			// All versions
			versions, _ := srcPlans.ListVersionHistory(ctx, sp.ID)
			for _, sv := range versions {
				nv := &models.PlanVersion{
					ID:         ids.New(),
					PlanID:     newPlanID,
					Version:    sv.Version,
					Title:      sv.Title,
					Body:       sv.Body,
					Actor:      actor,
					ChangeNote: sv.ChangeNote,
					CreatedAt:  now,
				}
				if !dryRun {
					dstPlans.InsertVersion(ctx, nv) //nolint:errcheck
				}
			}
		}

		// Comments
		srcCommentList, _ := srcComments.ListForTask(ctx, st.ID)
		for _, sc := range srcCommentList {
			nc := &models.Comment{
				ID:        ids.New(),
				TaskID:    newID,
				Body:      sc.Body,
				Actor:     actor,
				CreatedAt: timeutil.Now(),
			}
			if !dryRun {
				dstComments.Insert(ctx, nc) //nolint:errcheck
			}
			result.Comments++
		}
	}

	// Dependencies, once every task exists. An edge to a task outside the
	// import (another project under --project) is left out.
	edges, err := extrepo.NewDependencyRepo(srcDB).Edges(ctx)
	if err != nil {
		return nil, fmt.Errorf("import: list source dependencies: %w", err)
	}
	for _, e := range edges {
		taskID, ok1 := taskMap[e[0]]
		depID, ok2 := taskMap[e[1]]
		if ok1 && !ok2 {
			result.SkippedDependencies++
		}
		if !ok1 || !ok2 {
			continue
		}
		if !dryRun {
			if _, err := dstDeps.Add(ctx, dstDB, taskID, depID, timeutil.Now()); err != nil {
				return nil, fmt.Errorf("import: insert dependency: %w", err)
			}
		}
		result.Dependencies++
	}

	return result, nil
}
