package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/service"
)

// insertDoc writes a doc with a fixed ID, so tests control the prefixes.
func insertDoc(t *testing.T, ctx context.Context, docs *repo.DocRepo, projectID, id, title string) {
	t.Helper()
	require.NoError(t, docs.Insert(ctx, &models.Doc{
		ID: id, ProjectID: projectID, Title: title, CurrentVersion: 1, Actor: testActor,
	}))
	require.NoError(t, docs.InsertVersion(ctx, &models.DocVersion{
		ID: id + "V", DocID: id, Version: 1, Title: title, Body: "body " + title, Actor: testActor,
	}))
}

func TestDocShortIDPrefix(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	p, err := service.NewProjectService(db).Create(ctx, models.CreateProjectInput{Alias: "proj", Name: "Proj", Actor: testActor})
	require.NoError(t, err)

	docs := repo.NewDocRepo(db)
	const unique = "01AAAAAAAA0000000000000001"
	const twinA = "01BBBBBBBB1000000000000001"
	const twinB = "01BBBBBBBB2000000000000002"
	insertDoc(t, ctx, docs, p.ID, unique, "unique")
	insertDoc(t, ctx, docs, p.ID, twinA, "twin a")
	insertDoc(t, ctx, docs, p.ID, twinB, "twin b")

	svc := service.NewDocService(db)

	for _, ref := range []string{unique, unique[:8], strings.ToLower(unique[:8]), unique[:12]} {
		d, err := svc.Get(ctx, ref)
		require.NoError(t, err, ref)
		require.Equal(t, unique, d.ID, ref)
	}

	_, err = svc.Get(ctx, twinA[:8])
	require.ErrorContains(t, err, "matches more than one doc")

	d, err := svc.Get(ctx, twinA[:11])
	require.NoError(t, err)
	require.Equal(t, twinA, d.ID)

	for _, ref := range []string{unique[:7], "01AAAAA%", "01AAAAA_A", "01CCCCCCCC"} {
		_, err := svc.Get(ctx, ref)
		require.ErrorContains(t, err, "doc not found", ref)
	}

	// Update must write the new version to the full ID, not to the prefix.
	_, err = svc.Append(ctx, unique[:8], "more", "", testActor)
	require.NoError(t, err)
	d, err = svc.Get(ctx, unique)
	require.NoError(t, err)
	require.Equal(t, 2, d.CurrentVersion)
	require.Equal(t, "body unique\nmore", d.Version.Body)

	hist, err := svc.History(ctx, unique[:8])
	require.NoError(t, err)
	require.Len(t, hist, 2)

	require.NoError(t, svc.Delete(ctx, unique[:8], testActor))
	_, err = svc.Get(ctx, unique)
	require.Error(t, err)
}

func TestShortIDPrefixOtherEntities(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	p, err := service.NewProjectService(db).Create(ctx, models.CreateProjectInput{Alias: "proj", Name: "Proj", Actor: testActor})
	require.NoError(t, err)
	planSvc := service.NewPlanService(db)
	taskSvc := service.NewTaskService(db, planSvc, service.NewLabelService(db))
	task, err := taskSvc.Create(ctx, models.CreateTaskInput{ProjectID: p.ID, Title: "task", Actor: testActor})
	require.NoError(t, err)

	memSvc := service.NewMemoryService(db)
	m, err := memSvc.Add(ctx, models.CreateMemoryInput{ProjectID: p.ID, Body: "fact", Actor: testActor})
	require.NoError(t, err)
	m2, err := memSvc.Append(ctx, m.ID[:8], "more", testActor)
	require.NoError(t, err)
	require.Equal(t, m.ID, m2.ID)
	require.Equal(t, "fact\nmore", m2.Body)

	pl, err := planSvc.Create(ctx, models.CreatePlanInput{TaskID: task.ID, Title: "plan", Body: "steps", Actor: testActor})
	require.NoError(t, err)
	got, err := planSvc.Get(ctx, pl.ID[:8], 0)
	require.NoError(t, err)
	require.Equal(t, pl.ID, got.ID)

	attSvc := service.NewAttachmentService(db)
	a, err := attSvc.Add(ctx, "a.txt", []byte("x"), "task", task.ID, testActor)
	require.NoError(t, err)
	ga, err := attSvc.Get(ctx, a.ID[:8])
	require.NoError(t, err)
	require.Equal(t, a.ID, ga.ID)

	commentSvc := service.NewCommentService(db)
	c, err := commentSvc.Create(ctx, models.CreateCommentInput{TaskID: task.ID, Body: "hi", Actor: testActor})
	require.NoError(t, err)
	require.NoError(t, commentSvc.Delete(ctx, c.ID[:8], testActor))
	left, err := commentSvc.ListForTask(ctx, task.ID)
	require.NoError(t, err)
	require.Empty(t, left)

	require.NoError(t, memSvc.Delete(ctx, m.ID[:8]))
	require.NoError(t, planSvc.Delete(ctx, pl.ID[:8], testActor))
	require.NoError(t, attSvc.Delete(ctx, a.ID[:8], testActor))
}
