package view

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
)

// integrationSetup bootstraps schema + migrations + project.Repository,
// mirroring the other integration fixtures.
func integrationSetup(
	t *testing.T,
) (*project.Repository, *Service, context.Context) {
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

	schema := "test_view_" + fmt.Sprintf("%d", time.Now().UnixNano())

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

	projectRepo := project.NewRepository(pool, queries)

	service := NewService(
		NewRepository(queries),
		project.NewAuthorizer(projectRepo),
	)

	return projectRepo, service, ctx
}

func setupTenant(
	t *testing.T,
	projectRepo *project.Repository,
	ctx context.Context,
) project.Project {
	t.Helper()

	const tenant = "tenant-views"

	item, err := projectRepo.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		Name:            "views project",
		CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := projectRepo.AddMember(ctx, project.AddMemberParams{
		TenantID:  tenant,
		ProjectID: item.ID,
		UserID:    "member-user",
		Role:      "member",
	}); err != nil {
		t.Fatal(err)
	}

	return item
}

func TestSavedViewLifecycle(t *testing.T) {
	projectRepo, service, ctx := integrationSetup(t)

	item := setupTenant(t, projectRepo, ctx)

	member := auth.Identity{
		UserID:   "member-user",
		TenantID: "tenant-views",
	}

	stranger := auth.Identity{
		UserID:   "stranger",
		TenantID: "tenant-views",
	}

	// Create with filters, matching the UI save-view payload.
	created, err := service.Create(ctx, CreateInput{
		Identity: member,

		ProjectID: item.ID,

		Name:     "High priority",
		ViewType: "board",

		Filters: []Filter{
			{
				Field:    "priority",
				Operator: "is",
				Values:   []string{"urgent", "high"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if created.Name != "High priority" ||
		created.ViewType != "board" ||
		created.CreatedByUserID != "member-user" {

		t.Fatalf("created view wrong: %+v", created)
	}

	if len(created.Filters) != 1 ||
		created.Filters[0].Field != "priority" ||
		len(created.Filters[0].Values) != 2 {

		t.Fatalf("created filters wrong: %+v", created.Filters)
	}

	// Duplicate names conflict within the project.
	if _, err := service.Create(ctx, CreateInput{
		Identity: member,

		ProjectID: item.ID,

		Name: "High priority",
	}); !errors.Is(err, ErrViewConflict) {
		t.Fatalf("duplicate name err = %v, want ErrViewConflict", err)
	}

	// Validation: unknown field, unknown op, bad type.
	badField := []Filter{{Field: "magic"}}
	if _, err := service.Create(ctx, CreateInput{
		Identity: member,

		ProjectID: item.ID,
		Name:      "bad",

		Filters: badField,
	}); !errors.Is(err, ErrViewInvalidFilter) {

		t.Fatalf("bad field err = %v", err)
	}

	badOp := []Filter{{Field: "priority", Operator: "starts"}}
	if _, err := service.Create(ctx, CreateInput{
		Identity: member,

		ProjectID: item.ID,
		Name:      "bad2",

		Filters: badOp,
	}); !errors.Is(err, ErrViewInvalidFilter) {

		t.Fatalf("bad op err = %v", err)
	}

	tooMany := make([]Filter, maxFilters+1)
	for i := range tooMany {
		tooMany[i] = Filter{Field: "priority"}
	}

	if _, err := service.Create(ctx, CreateInput{
		Identity: member,

		ProjectID: item.ID,
		Name:      "bad3",

		Filters: tooMany,
	}); !errors.Is(err, ErrViewFiltersTooMany) {

		t.Fatalf("too many filters err = %v", err)
	}

	if _, err := service.Create(ctx, CreateInput{
		Identity: member,

		ProjectID: item.ID,
		Name:      "bad4",

		ViewType: "kanban",
	}); !errors.Is(err, ErrViewInvalidType) {

		t.Fatalf("bad type err = %v", err)
	}

	// A second view, from another member's data.
	second, err := service.Create(ctx, CreateInput{
		Identity: member,

		ProjectID: item.ID,

		Name: "Spillover",
	})
	if err != nil {
		t.Fatal(err)
	}

	if second.ViewType != "list" || len(second.Filters) != 0 {
		t.Fatalf("defaults wrong: %+v", second)
	}

	// List is creation-ordered.
	views, err := service.List(ctx, ListInput{
		Identity:  member,
		ProjectID: item.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(views) != 2 || views[0].Name != "High priority" || views[1].Name != "Spillover" {
		t.Fatalf("list order wrong: %+v", views)
	}

	// Outsiders cannot read the list.
	if _, err := service.List(ctx, ListInput{
		Identity:  stranger,
		ProjectID: item.ID,
	}); !errors.Is(err, project.ErrForbidden) &&
		!errors.Is(err, project.ErrNotFound) {

		t.Fatalf("stranger list err = %v", err)
	}

	// Rename keeps everything else.
	renamed, err := service.Update(ctx, UpdateInput{
		Identity: member,

		ProjectID: item.ID,
		ViewID:    created.ID,

		Name: strPtr("Urgent & high"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if renamed.Name != "Urgent & high" ||
		renamed.ViewType != "board" ||
		len(renamed.Filters) != 1 {

		t.Fatalf("rename lost data: %+v", renamed)
	}

	// Update filters with defaults filled in (op defaults to is).
	updated, err := service.Update(ctx, UpdateInput{
		Identity: member,

		ProjectID: item.ID,
		ViewID:    created.ID,

		Filters: &[]Filter{
			{Field: "status", Values: []string{""}},
			{Field: "assignee", Values: []string{"none"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(updated.Filters) != 2 || updated.Filters[0].Operator != "is" {
		t.Fatalf("filter update wrong: %+v", updated.Filters)
	}

	// Empty name is rejected; nothing-to-update surfaces.
	if _, err := service.Update(ctx, UpdateInput{
		Identity: member,

		ProjectID: item.ID,
		ViewID:    second.ID,
	}); !errors.Is(err, ErrNothingToUpdate) {

		t.Fatalf("no-op update err = %v", err)
	}

	// Delete.
	if err := service.Delete(ctx, DeleteInput{
		Identity: member,

		ProjectID: item.ID,
		ViewID:    second.ID,
	}); err != nil {
		t.Fatal(err)
	}

	views, err = service.List(ctx, ListInput{
		Identity:  member,
		ProjectID: item.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(views) != 1 {
		t.Fatalf("view count after delete = %d, want 1", len(views))
	}

	if err := service.Delete(ctx, DeleteInput{
		Identity: member,

		ProjectID: item.ID,
		ViewID:    second.ID,
	}); !errors.Is(err, ErrViewNotFound) {

		t.Fatalf("double delete err = %v", err)
	}
}

func strPtr(value string) *string {
	return &value
}
