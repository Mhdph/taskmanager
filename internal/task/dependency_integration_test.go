package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/project"
)

func integrationTaskRepository(t *testing.T) (*Repository, *project.Repository, context.Context) {
	t.Helper()
	url := os.Getenv("TASKMANAGER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TASKMANAGER_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	return NewRepository(pool, queries), project.NewRepository(pool, queries), ctx
}

func TestConcurrentInverseDependenciesCannotBothCommit(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	projectItem, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", Name: "dependencies", CreatedByUserID: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", projectItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	create := func(title string) Task {
		item, err := tasks.Create(ctx, CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", ProjectID: projectItem.ID, StatusID: statuses[0].ID, Title: title, Priority: PriorityMedium, CreatedByUserID: "manager"})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	a, b := create("a"), create("b")
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, pair := range [][2]string{{a.ID, b.ID}, {b.ID, a.ID}} {
		wg.Add(1)
		go func(i int, pair [2]string) {
			defer wg.Done()
			<-start
			_, errs[i] = tasks.AddDependency(ctx, "tenant-a", projectItem.ID, pair[0], pair[1])
		}(i, pair)
	}
	close(start)
	wg.Wait()
	if errors.Is(errs[0], ErrDependencyCycle) == errors.Is(errs[1], ErrDependencyCycle) || (errs[0] != nil && !errors.Is(errs[0], ErrDependencyCycle)) || (errs[1] != nil && !errors.Is(errs[1], ErrDependencyCycle)) {
		t.Fatalf("expected exactly one inverse edge to be rejected as a cycle, got %v", errs)
	}
	dependencies, err := tasks.ListDependencies(ctx, "tenant-a", projectItem.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := tasks.ListDependencies(ctx, "tenant-a", projectItem.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies)+len(other) != 1 {
		t.Fatalf("committed dependency count = %d, want 1", len(dependencies)+len(other))
	}
}

func TestDependencyDomainErrorsAndStatusIntegrity(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	projectItem, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", Name: "integrity", CreatedByUserID: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", projectItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	create := func(projectID, statusID string, parent *string) Task {
		item, err := tasks.Create(ctx, CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", ProjectID: projectID, StatusID: statusID, ParentTaskID: parent, Title: "task", Priority: PriorityMedium, CreatedByUserID: "manager"})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	a, b, c := create(projectItem.ID, statuses[0].ID, nil), create(projectItem.ID, statuses[0].ID, nil), create(projectItem.ID, statuses[0].ID, nil)
	if _, err := tasks.AddDependency(ctx, "tenant-a", projectItem.ID, a.ID, a.ID); !errors.Is(err, ErrSelfDependency) {
		t.Fatalf("self dependency error = %v", err)
	}
	if _, err := tasks.AddDependency(ctx, "tenant-a", projectItem.ID, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.AddDependency(ctx, "tenant-a", projectItem.ID, a.ID, b.ID); !errors.Is(err, ErrDependencyExists) {
		t.Fatalf("duplicate dependency error = %v", err)
	}
	if _, err := tasks.AddDependency(ctx, "tenant-a", projectItem.ID, b.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.AddDependency(ctx, "tenant-a", projectItem.ID, c.ID, a.ID); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("transitive cycle error = %v", err)
	}
	if err := projects.DeleteStatus(ctx, "tenant-a", projectItem.ID, statuses[0].ID); !errors.Is(err, project.ErrStatusInUse) {
		t.Fatalf("in-use status error = %v", err)
	}

	order := []string{statuses[2].ID, statuses[0].ID, statuses[1].ID}
	if err := projects.ReorderStatuses(ctx, "tenant-a", projectItem.ID, order); err != nil {
		t.Fatal(err)
	}
	updated, err := projects.ListStatuses(ctx, "tenant-a", projectItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, status := range updated {
		if status.ID != order[i] || status.Position != int32(i) {
			t.Fatalf("status[%d]=(%s,%d), want (%s,%d)", i, status.ID, status.Position, order[i], i)
		}
	}
	if err := projects.ReorderStatuses(ctx, "tenant-a", projectItem.ID, []string{order[0], order[0], order[2]}); !errors.Is(err, project.ErrInvalidStatusOrder) {
		t.Fatalf("duplicate reorder id error = %v", err)
	}

	otherProject, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", Name: "other", CreatedByUserID: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	otherStatuses, err := projects.ListStatuses(ctx, "tenant-a", otherProject.ID)
	if err != nil {
		t.Fatal(err)
	}
	parent := a.ID
	if _, err := tasks.Create(ctx, CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", ProjectID: otherProject.ID, StatusID: otherStatuses[0].ID, ParentTaskID: &parent, Title: "cross project", Priority: PriorityMedium, CreatedByUserID: "manager"}); err == nil {
		t.Fatal("cross-project parent task unexpectedly created")
	}
	otherTask := createTaskForTest(t, tasks, ctx, otherProject.ID, otherStatuses[0].ID)
	startedAt := time.Now().UTC()
	if _, err := tasks.db.Exec(ctx, `INSERT INTO task_time_entries (id,tenant_id,project_id,task_id,user_id,started_at) VALUES ($1,$2,$3,$4,$5,$6)`, uuid.New(), "tenant-a", projectItem.ID, a.ID, "timer-user", startedAt); err != nil {
		t.Fatal(err)
	}
	_, err = tasks.db.Exec(ctx, `INSERT INTO task_time_entries (id,tenant_id,project_id,task_id,user_id,started_at) VALUES ($1,$2,$3,$4,$5,$6)`, uuid.New(), "tenant-a", otherProject.ID, otherTask.ID, "timer-user", startedAt)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second active timer error = %v, want unique violation", err)
	}
}

func createTaskForTest(t *testing.T, repo *Repository, ctx context.Context, projectID, statusID string) Task {
	t.Helper()
	item, err := repo.Create(ctx, CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", ProjectID: projectID, StatusID: statusID, Title: "timer task", Priority: PriorityMedium, CreatedByUserID: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	return item
}
