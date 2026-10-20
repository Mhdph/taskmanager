package sprint

import (
	"context"
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

// integrationSprintRepository creates an isolated schema with the
// application migrations applied.
func integrationSprintRepository(
	t *testing.T,
) (*Repository, *pgxpool.Pool, context.Context) {
	t.Helper()

	url := os.Getenv("TASKMANAGER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TASKMANAGER_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		45*time.Second,
	)
	t.Cleanup(cancel)

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}

	schema := "test_" + uuid.NewString()[0:8]

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	if _, err := admin.Exec(
		ctx,
		`CREATE SCHEMA `+schema,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(
			context.Background(),
			`DROP SCHEMA `+schema+` CASCADE`,
		)
	})

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

	return NewRepository(pool, queries), pool, ctx
}

func integrationSprintService(
	t *testing.T,
) (*Service, *pgxpool.Pool, context.Context) {
	t.Helper()

	repository, pool, ctx := integrationSprintRepository(t)

	projectRepository := project.NewRepository(pool, db.New(pool))

	service := NewService(
		repository,
		projectRepository,
		project.NewAuthorizer(projectRepository),
		nil,
	)

	return service, pool, ctx
}

const sprintTestTenant = "tenant-a"

func insertSprintProject(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	projectID string,
) string {
	t.Helper()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO projects (id, tenant_id, name, created_by_user_id)
		 VALUES ($1, $2, 'sprint project', 'alice')`,
		projectID,
		sprintTestTenant,
	); err != nil {
		t.Fatal(err)
	}

	statusID := uuid.NewString()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO project_statuses (id, tenant_id, project_id, name, position, category)
		 VALUES ($1, $2, $3, 'todo', 0, 'todo')`,
		statusID,
		sprintTestTenant,
		projectID,
	); err != nil {
		t.Fatal(err)
	}

	return statusID
}

func insertSprintMember(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	projectID string,
	userID string,
	role string,
) {
	t.Helper()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO project_members (project_id, tenant_id, user_id, role)
		 VALUES ($1, $2, $3, $4)`,
		projectID,
		sprintTestTenant,
		userID,
		role,
	); err != nil {
		t.Fatal(err)
	}
}

func insertSprintTask(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	projectID string,
	statusID string,
	title string,
	estimateSeconds *int64,
	completed bool,
) string {
	t.Helper()

	taskID := uuid.NewString()

	var completedAt any
	if completed {
		completedAt = time.Now()
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tasks (
			id, tenant_id, project_id, status_id, title,
			created_by_user_id, completed_at, estimate_seconds
		 ) VALUES ($1, $2, $3, $4, $5, 'alice', $6, $7)`,
		taskID,
		sprintTestTenant,
		projectID,
		statusID,
		title,
		completedAt,
		estimateSeconds,
	); err != nil {
		t.Fatal(err)
	}

	return taskID
}

func sprintIdentity(userID string) auth.Identity {
	return auth.Identity{
		TenantID: sprintTestTenant,
		UserID:   userID,
	}
}

func estimate(value int64) *int64 {
	return &value
}
