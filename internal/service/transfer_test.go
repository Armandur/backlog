package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/service"
)

func TestTaskTransfer(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	projSvc := service.NewProjectService(db)
	labelSvc := service.NewLabelService(db)
	taskSvc := service.NewTaskService(db, service.NewPlanService(db), labelSvc)
	commentSvc := service.NewCommentService(db)

	src, _ := projSvc.Create(ctx, models.CreateProjectInput{Alias: "src", Name: "Src", Actor: testActor})
	dst, _ := projSvc.Create(ctx, models.CreateProjectInput{Alias: "dst", Name: "Dst", Actor: testActor})
	// "shared" already exists in the target, "fresh" does not.
	existing, err := labelSvc.Create(ctx, models.CreateLabelInput{ProjectID: dst.ID, Name: "shared"})
	require.NoError(t, err)

	task, err := taskSvc.Create(ctx, models.CreateTaskInput{
		ProjectID: src.ID, Title: "movable", Labels: []string{"shared", "fresh"}, Actor: testActor,
	})
	require.NoError(t, err)
	stay, _ := taskSvc.Create(ctx, models.CreateTaskInput{
		ProjectID: src.ID, Title: "stays", Labels: []string{"shared"}, Actor: testActor,
	})
	_, err = commentSvc.Create(ctx, models.CreateCommentInput{TaskID: task.ID, Body: "note", Actor: testActor})
	require.NoError(t, err)

	moved, err := taskSvc.Transfer(ctx, "TASK-1", "dst", testActor)
	require.NoError(t, err)
	require.Equal(t, dst.ID, moved.ProjectID)
	require.Equal(t, task.Seq, moved.Seq)

	got, err := taskSvc.Get(ctx, task.ID, false, true)
	require.NoError(t, err)
	require.Len(t, got.Comments, 1)
	require.Len(t, got.Labels, 2)
	for _, l := range got.Labels {
		require.Equal(t, dst.ID, l.ProjectID, "label %s must belong to the target project", l.Name)
		if l.Name == "shared" {
			require.Equal(t, existing.ID, l.ID, "existing target label must be reused")
		}
	}

	// The task left behind keeps its own project's label.
	left, _ := taskSvc.Get(ctx, stay.ID, false, false)
	require.Len(t, left.Labels, 1)
	require.Equal(t, src.ID, left.Labels[0].ProjectID)

	srcTasks, _, _ := taskSvc.List(ctx, models.TaskFilter{ProjectAlias: "src"})
	require.Len(t, srcTasks, 1)
	dstTasks, _, _ := taskSvc.List(ctx, models.TaskFilter{ProjectAlias: "dst"})
	require.Len(t, dstTasks, 1)

	_, err = taskSvc.Transfer(ctx, "TASK-1", "dst", testActor)
	require.ErrorContains(t, err, "already in project")
	_, err = taskSvc.Transfer(ctx, "TASK-1", "missing", testActor)
	require.Error(t, err)
	_, err = projSvc.Archive(ctx, "src", testActor)
	require.NoError(t, err)
	_, err = taskSvc.Transfer(ctx, "TASK-1", "src", testActor)
	require.ErrorContains(t, err, "archived")
}

func TestDocTransfer(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	projSvc := service.NewProjectService(db)
	docSvc := service.NewDocService(db)

	projSvc.Create(ctx, models.CreateProjectInput{Alias: "src", Name: "Src", Actor: testActor})
	dst, _ := projSvc.Create(ctx, models.CreateProjectInput{Alias: "dst", Name: "Dst", Actor: testActor})

	d, err := docSvc.Create(ctx, "src", models.CreateDocInput{Title: "Report", Body: "v1", Actor: testActor})
	require.NoError(t, err)
	_, err = docSvc.Append(ctx, d.ID, "more", "", testActor)
	require.NoError(t, err)

	moved, err := docSvc.Transfer(ctx, d.ID, "dst", testActor)
	require.NoError(t, err)
	require.Equal(t, dst.ID, moved.ProjectID)
	require.Equal(t, 2, moved.CurrentVersion)

	srcDocs, _ := docSvc.List(ctx, "src")
	require.Empty(t, srcDocs)
	dstDocs, _ := docSvc.List(ctx, "dst")
	require.Len(t, dstDocs, 1)
	history, _ := docSvc.History(ctx, d.ID)
	require.Len(t, history, 2)

	_, err = docSvc.Transfer(ctx, d.ID, "dst", testActor)
	require.ErrorContains(t, err, "already in project")
}
