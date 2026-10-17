package favorite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

func integrationSetup(
	t *testing.T,
) (*Repository, *task.Repository, *project.Repository, *project.Authorizer, context.Context) {
	t.Helper()

	url := os.Getenv("TASKMANAGER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TASKMANAGER_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}

	schema := "test_favorite_" + fmt.Sprintf("%d", time.Now().UnixNano())

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) })

	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	files, err := filepath.Glob("../../db/migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(files)

	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}

	queries := db.New(pool)

	return NewRepository(pool, queries),
		task.NewRepository(pool, queries),
		project.NewRepository(pool, queries),
		project.NewAuthorizer(project.NewRepository(pool, queries)),
		ctx
}

func TestFavoritesTasksAndProjects(t *testing.T) {
	repo, taskRepo, projectRepo, authorizer, ctx := integrationSetup(t)

	service := NewService(repo, taskRepo, projectRepo, authorizer)

	const tenant = "tenant-fav"

	projectItem, err := projectRepo.CreateWithDefaultStatuses(
		ctx,
		project.CreateParams{
			ID:              uuid.NewString(),
			TenantID:        tenant,
			Name:            "fav project",
			CreatedByUserID: "owner-user",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	statuses, err := projectRepo.ListStatuses(ctx, tenant, projectItem.ID)
	if err != nil {
		t.Fatal(err)
	}

	created, err := taskRepo.Create(ctx, task.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		ProjectID:       projectItem.ID,
		StatusID:        statuses[0].ID,
		Title:           "fav task",
		Priority:        task.PriorityMedium,
		CreatedByUserID: "owner-user",
	})
	if err != nil {
		t.Fatal(err)
	}

	owner := auth.Identity{
		UserID:   "owner-user",
		TenantID: tenant,
	}

	stranger := auth.Identity{
		UserID:   "stranger-user",
		TenantID: tenant,
	}

	// Favorites start empty.
	list, err := service.List(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}

	if len(list.TaskIDs) != 0 || len(list.ProjectIDs) != 0 {
		t.Fatalf("fresh favorites not empty: %+v", list)
	}

	// Project favorite.
	if err := service.AddProject(ctx, owner, projectItem.ID); err != nil {
		t.Fatal(err)
	}

	// Adding again is idempotent.
	if err := service.AddProject(ctx, owner, projectItem.ID); err != nil {
		t.Fatal(err)
	}

	// Task favorite.
	if err := service.AddTask(ctx, owner, created.ID); err != nil {
		t.Fatal(err)
	}

	list, err = service.List(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}

	if len(list.TaskIDs) != 1 || list.TaskIDs[0] != created.ID {
		t.Fatalf("task favorites = %v, want [%s]", list.TaskIDs, created.ID)
	}

	if len(list.ProjectIDs) != 1 || list.ProjectIDs[0] != projectItem.ID {
		t.Fatalf("project favorites = %v, want [%s]", list.ProjectIDs, projectItem.ID)
	}

	// A user without project membership is hidden from favorites: the
	// authorizer surfaces either forbidden or not-found.
	if err := service.AddProject(ctx, stranger, projectItem.ID); !errors.Is(err, project.ErrForbidden) &&
		!errors.Is(err, project.ErrNotFound) {

		t.Fatalf("stranger add project err = %v, want forbidden or not-found", err)
	}

	if err := service.AddTask(ctx, stranger, created.ID); !errors.Is(err, project.ErrForbidden) &&
		!errors.Is(err, project.ErrNotFound) {

		t.Fatalf("stranger add task err = %v, want forbidden or not-found", err)
	}

	// Unknown entities are rejected.
	if err := service.AddTask(ctx, owner, uuid.NewString()); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("unknown task err = %v, want ErrTaskNotFound", err)
	}

	if err := service.AddProject(ctx, owner, uuid.NewString()); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("unknown project err = %v, want ErrNotFound", err)
	}

	// Removal clears the favorites.
	if err := service.RemoveTask(ctx, owner, created.ID); err != nil {
		t.Fatal(err)
	}

	// Removing again reports not-found.
	if err := service.RemoveTask(ctx, owner, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double remove err = %v, want ErrNotFound", err)
	}

	if err := service.RemoveProject(ctx, owner, projectItem.ID); err != nil {
		t.Fatal(err)
	}

	list, err = service.List(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}

	if len(list.TaskIDs) != 0 || len(list.ProjectIDs) != 0 {
		t.Fatalf("favorites not cleared: %+v", list)
	}
}
