package project

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
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

func integrationProjectRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	url := os.Getenv("TASKMANAGER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TASKMANAGER_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	return NewRepository(pool, db.New(pool)), ctx
}

func TestConcurrentManagerRemovalLeavesOneManager(t *testing.T) {
	repo, ctx := integrationProjectRepository(t)
	tenantID, firstID, secondID := "tenant-a", "manager-a", "manager-b"
	created, err := repo.CreateWithDefaultStatuses(ctx, CreateParams{ID: uuid.NewString(), TenantID: tenantID, Name: "race", CreatedByUserID: firstID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, AddMemberParams{TenantID: tenantID, ProjectID: created.ID, UserID: secondID, Role: MemberRoleManager}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, userID := range []string{firstID, secondID} {
		wg.Add(1)
		go func(i int, userID string) {
			defer wg.Done()
			<-start
			errs[i] = repo.DeleteMember(ctx, tenantID, created.ID, userID)
		}(i, userID)
	}
	close(start)
	wg.Wait()
	if errors.Is(errs[0], ErrLastManager) == errors.Is(errs[1], ErrLastManager) || (errs[0] != nil && !errors.Is(errs[0], ErrLastManager)) || (errs[1] != nil && !errors.Is(errs[1], ErrLastManager)) {
		t.Fatalf("expected exactly one final-manager rejection, got %v", errs)
	}
	count, err := repo.CountManagers(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("manager count = %d, want 1", count)
	}
}

func TestConcurrentManagerDowngradeLeavesOneManager(t *testing.T) {
	repo, ctx := integrationProjectRepository(t)
	tenantID, firstID, secondID := "tenant-a", "manager-a", "manager-b"
	created, err := repo.CreateWithDefaultStatuses(ctx, CreateParams{ID: uuid.NewString(), TenantID: tenantID, Name: "race", CreatedByUserID: firstID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMember(ctx, AddMemberParams{TenantID: tenantID, ProjectID: created.ID, UserID: secondID, Role: MemberRoleManager}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, userID := range []string{firstID, secondID} {
		wg.Add(1)
		go func(i int, userID string) {
			defer wg.Done()
			<-start
			_, errs[i] = repo.UpdateMemberRole(ctx, tenantID, created.ID, userID, MemberRoleViewer)
		}(i, userID)
	}
	close(start)
	wg.Wait()
	if errors.Is(errs[0], ErrLastManager) == errors.Is(errs[1], ErrLastManager) || (errs[0] != nil && !errors.Is(errs[0], ErrLastManager)) || (errs[1] != nil && !errors.Is(errs[1], ErrLastManager)) {
		t.Fatalf("expected exactly one final-manager rejection, got %v", errs)
	}
	count, err := repo.CountManagers(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("manager count = %d, want 1", count)
	}
}
