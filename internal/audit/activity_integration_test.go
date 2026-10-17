package audit

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

func integrationSetup(
	t *testing.T,
) (*pgxpool.Pool, *Repository, *project.Repository, *Service, context.Context) {
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

	schema := "test_audit_" + fmt.Sprintf("%d", time.Now().UnixNano())

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

	repo := NewRepository(queries)

	projectRepo := project.NewRepository(pool, queries)
	authorizer := project.NewAuthorizer(projectRepo)

	return pool, repo, projectRepo, NewService(repo, authorizer), ctx
}

func seedEntry(
	t *testing.T,
	pool *pgxpool.Pool,
	tenantID string,
	actorUserID string,
	action Action,
	entityType EntityType,
	projectID *string,
	taskID *string,
	createdAt time.Time,
) {
	t.Helper()

	id := uuid.NewString()

	metadata := `{"changed_fields":["title"]}`

	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO audit_logs (
			id, tenant_id, actor_user_id, action, entity_type,
			entity_id, project_id, task_id, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)`,
		id, tenantID, actorUserID, string(action), string(entityType),
		id, projectID, taskID, metadata, createdAt,
	); err != nil {
		t.Fatalf("seed audit entry: %v", err)
	}
}

func TestActivityFeedScoping(t *testing.T) {
	pool, _, projectRepo, service, ctx := integrationSetup(t)

	const tenant = "tenant-act"

	projectA, err := projectRepo.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		Name:            "visible project",
		CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := projectRepo.AddMember(ctx, project.AddMemberParams{
		TenantID:  tenant,
		ProjectID: projectA.ID,
		UserID:    "member-user",
		Role:      "member",
	}); err != nil {
		t.Fatal(err)
	}

	projectB, err := projectRepo.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		Name:            "hidden project",
		CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Add(-time.Hour)

	projectAID := projectA.ID
	projectBID := projectB.ID

	seedEntry(t, pool, tenant, "manager", ActionTaskCreated, EntityTask, &projectAID, nil, base)
	seedEntry(t, pool, tenant, "manager", ActionTaskUpdated, EntityTask, &projectAID, nil, base.Add(time.Minute))
	seedEntry(t, pool, tenant, "manager", ActionCommentCreated, EntityComment, &projectAID, nil, base.Add(2*time.Minute))
	seedEntry(t, pool, tenant, "manager", ActionProjectCreated, EntityProject, &projectBID, nil, base.Add(3*time.Minute))
	seedEntry(t, pool, tenant, "manager", ActionTaskStatusChanged, EntityTask, &projectBID, nil, base.Add(4*time.Minute))

	member := auth.Identity{
		UserID:   "member-user",
		TenantID: tenant,
	}

	// Members only see the projects they belong to.
	entries, _, err := service.View(ctx, ViewOptions{Identity: member})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 3 {
		t.Fatalf("member feed count = %d, want 3", len(entries))
	}

	for _, entry := range entries {
		if entry.ProjectID != nil { // always non-nil here
			if *entry.ProjectID == projectB.ID {
				t.Fatalf("member saw a project B entry")
			}
		}
	}

	// Newest first.
	if *entries[0].ProjectID != projectA.ID || entries[0].Action != ActionCommentCreated {
		t.Fatalf("feed not newest first: %+v", entries[0])
	}

	admin := auth.Identity{
		UserID:   "admin-user",
		TenantID: tenant,
		Role:     auth.RoleAdmin,
	}

	adminEntries, _, err := service.View(ctx, ViewOptions{Identity: admin})
	if err != nil {
		t.Fatal(err)
	}

	if len(adminEntries) != 5 {
		t.Fatalf("admin feed count = %d, want 5", len(adminEntries))
	}

	// A user without membership gets 404-ish behavior on a project feed.
	stranger := auth.Identity{
		UserID:   "stranger-user",
		TenantID: tenant,
	}

	if _, _, err := service.View(ctx, ViewOptions{Identity: stranger, ProjectID: &projectA.ID}); err == nil {
		t.Fatal("stranger project feed succeeded, want error")
	}

	// Members of the project can read its feed via the project path.
	projectEntries, _, err := service.View(ctx, ViewOptions{
		Identity:  member,
		ProjectID: &projectA.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(projectEntries) != 3 {
		t.Fatalf("project feed count = %d, want 3", len(projectEntries))
	}

	// The tenant-wide list already excludes project B for the member, so
	// the project filter simply narrows further.
	filteredByProject, _, err := service.View(ctx, ViewOptions{
		Identity:  admin,
		ProjectID: &projectB.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(filteredByProject) != 2 {
		t.Fatalf("admin filter-by-project count = %d, want 2", len(filteredByProject))
	}
}

func TestActivityFeedFiltersAndPagination(t *testing.T) {
	pool, _, _, service, ctx := integrationSetup(t)

	const tenant = "tenant-act2"

	base := time.Now().UTC().Add(-2 * time.Hour)

	projectFilter := uuid.NewString()
	actorFilter := "actor-one"

	// Seed 5 comment entries + 1 recorded-vs-other actor/entity mix.
	for i := 0; i < 5; i++ {
		seedEntry(t, pool, tenant, actorFilter, ActionCommentCreated, EntityComment, &projectFilter, nil, base.Add(time.Duration(i)*time.Minute))
	}

	otherActor := "actor-two"
	seedEntry(t, pool, tenant, otherActor, ActionProjectCreated, EntityProject, &projectFilter, nil, base.Add(10*time.Minute))

	actor := actorFilter
	entity := EntityComment

	entries, _, err := service.View(ctx, ViewOptions{
		Identity: auth.Identity{
			UserID:   "admin-user",
			TenantID: tenant,
			Role:     auth.RoleAdmin,
		},

		ActorUserID: &actor,
		EntityType:  &entity,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 5 {
		t.Fatalf("filtered count = %d, want 5", len(entries))
	}

	for _, entry := range entries {
		if entry.ActorUserID != actorFilter || entry.EntityType != EntityComment {
			t.Fatalf("filter did not apply: %+v", entry)
		}
	}

	// Stepping through with a cursor returns everything without loss or
	// duplication.
	seen := map[string]struct{}{}
	cursorToken := ""

	pages := 0

	for {
		options := ViewOptions{
			Identity: auth.Identity{
				UserID:   "admin-user",
				TenantID: tenant,
				Role:     auth.RoleAdmin,
			},

			ActorUserID: &actor,
			EntityType:  &entity,

			Limit: 2,
		}

		if cursorToken != "" {
			cursor, err := DecodeAuditCursor(cursorToken)
			if err != nil {
				t.Fatal(err)
			}

			options.Cursor = &cursor
		}

		pageEntries, next, err := service.View(ctx, options)
		if err != nil {
			t.Fatal(err)
		}

		for _, entry := range pageEntries {
			if _, ok := seen[entry.ID]; ok {
				t.Fatalf("entry %s returned twice", entry.ID)
			}

			seen[entry.ID] = struct{}{}
		}

		pages++

		if next == nil {
			break
		}

		token, err := EncodeAuditCursor(*next)
		if err != nil {
			t.Fatal(err)
		}

		cursorToken = token
	}

	if pages != 3 || len(seen) != 5 {
		t.Fatalf("pagination walked %d pages with %d entries, want 3 pages / 5 entries", pages, len(seen))
	}

	// Invalid entity types are rejected before the query runs.
	badType := EntityType("spaceship")

	if _, _, err := service.View(ctx, ViewOptions{
		Identity: auth.Identity{
			UserID:   "admin-user",
			TenantID: tenant,
			Role:     auth.RoleAdmin,
		},
		EntityType: &badType,
	}); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("bad entity type err = %v, want ErrInvalidFilter", err)
	}
}
