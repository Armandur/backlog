package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/service"
)

// TestDependencyChangesAreAllOrNothing checks that one rejected edge leaves
// no partial change behind, on update and on create.
func TestDependencyChangesAreAllOrNothing(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	proj, err := service.NewProjectService(db).Create(ctx, models.CreateProjectInput{
		Alias: "deps", Name: "Deps", Actor: testActor,
	})
	require.NoError(t, err)
	svc := service.NewTaskService(db, service.NewPlanService(db), service.NewLabelService(db))

	mk := func(title string, deps ...string) *models.Task {
		task, err := svc.Create(ctx, models.CreateTaskInput{
			ProjectID: proj.ID, Title: title, DependsOn: deps, Actor: testActor,
		})
		require.NoError(t, err)
		return task
	}
	a := mk("A")
	b := mk("B", "TASK-1")
	c := mk("C")
	require.Len(t, b.DependsOn, 1, "Create returns the stored dependency")

	// C may depend on A, but not on itself: the whole update must fail and
	// leave C without the A edge.
	_, err = svc.Update(ctx, "TASK-3", models.UpdateTaskInput{AddDependsOn: []string{"TASK-1", "TASK-3"}}, testActor)
	require.ErrorContains(t, err, "itself")
	got, err := svc.Get(ctx, c.ID, false, false)
	require.NoError(t, err)
	require.Empty(t, got.DependsOn)

	// A cycle in the middle of a list also rolls back the edges before it.
	_, err = svc.Update(ctx, "TASK-1", models.UpdateTaskInput{AddDependsOn: []string{"TASK-3", "TASK-2"}}, testActor)
	require.ErrorContains(t, err, "cycle")
	require.ErrorContains(t, err, "TASK-1 -> TASK-2 -> TASK-1")
	got, err = svc.Get(ctx, a.ID, false, false)
	require.NoError(t, err)
	require.Empty(t, got.DependsOn)

	// A failed create leaves no task behind.
	_, err = svc.Create(ctx, models.CreateTaskInput{
		ProjectID: proj.ID, Title: "E", DependsOn: []string{"TASK-99"}, Actor: testActor,
	})
	require.Error(t, err)
	all, total, err := svc.List(ctx, models.TaskFilter{ProjectAlias: "deps"})
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, all, 3)

	// --ready and its count agree when paginating.
	ready, total, err := svc.List(ctx, models.TaskFilter{ProjectAlias: "deps", Ready: true, Limit: 1})
	require.NoError(t, err)
	require.Len(t, ready, 1)
	require.Equal(t, 2, total, "A and C are ready, B waits for A")
}
