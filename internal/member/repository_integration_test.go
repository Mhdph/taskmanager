package member

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

func integrationMemberRepository(t *testing.T) (*Repository, context.Context) {
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
	schema := "test_member_" + fmt.Sprintf("%d", time.Now().UnixNano())
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

func TestSyncIsIdempotent(t *testing.T) {
	repo, ctx := integrationMemberRepository(t)

	first := SyncParams{
		TenantID:  "tenant-a",
		UserID:    "user-a",
		Email:     "a@example.com",
		FirstName: "First",
		LastName:  "Last",
	}

	if err := repo.Sync(ctx, first); err != nil {
		t.Fatal(err)
	}

	created, err := repo.Get(ctx, "tenant-a", "user-a")
	if err != nil {
		t.Fatal(err)
	}

	if created.Role != RoleMember {
		t.Fatalf("default role = %q, want %q", created.Role, RoleMember)
	}

	if created.Status != StatusActive {
		t.Fatalf("default status = %q, want %q", created.Status, StatusActive)
	}

	// Same values: upsert's WHERE clause must skip the write, leaving
	// created_at untouched.
	if err := repo.Sync(ctx, first); err != nil {
		t.Fatal(err)
	}

	synced, err := repo.Get(ctx, "tenant-a", "user-a")
	if err != nil {
		t.Fatal(err)
	}

	if synced.CreatedAt != created.CreatedAt {
		t.Fatal("re-sync with unchanged profile rewrote the row")
	}

	// Changed profile: name must be refreshed in the directory.
	updated := first
	updated.FirstName = "Renamed"

	if err := repo.Sync(ctx, updated); err != nil {
		t.Fatal(err)
	}

	synced, err = repo.Get(ctx, "tenant-a", "user-a")
	if err != nil {
		t.Fatal(err)
	}

	if synced.FirstName != "Renamed" {
		t.Fatalf("first name = %q, want %q", synced.FirstName, "Renamed")
	}

	// Manual role changes survive a re-sync with the same profile.
	if _, err := repo.UpdateRole(ctx, "tenant-a", "user-a", RoleOwner); err != nil {
		t.Fatal(err)
	}

	if err := repo.Sync(ctx, updated); err != nil {
		t.Fatal(err)
	}

	synced, err = repo.Get(ctx, "tenant-a", "user-a")
	if err != nil {
		t.Fatal(err)
	}

	if synced.Role != RoleOwner {
		t.Fatalf("role = %q, want %q (sync must not reset directory roles)", synced.Role, RoleOwner)
	}
}

func TestListFilters(t *testing.T) {
	repo, ctx := integrationMemberRepository(t)

	members := []SyncParams{
		{TenantID: "tenant-a", UserID: "user-a", Email: "anna@example.com", FirstName: "Anna", LastName: "Alpha"},
		{TenantID: "tenant-a", UserID: "user-b", Email: "bob@example.com", FirstName: "Bob", LastName: "Beta"},
		{TenantID: "tenant-a", UserID: "user-c", Email: "carol@example.com", FirstName: "Carol", LastName: "Gamma"},
	}

	for _, member := range members {
		if err := repo.Sync(ctx, member); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := repo.UpdateRole(ctx, "tenant-a", "user-b", RoleOwner); err != nil {
		t.Fatal(err)
	}

	all, err := repo.List(ctx, "tenant-a", ListOptions{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 3 {
		t.Fatalf("list length = %d, want 3", len(all))
	}

	owners, err := repo.List(ctx, "tenant-a", ListOptions{Limit: 500, Role: rolePtr(RoleOwner)})
	if err != nil {
		t.Fatal(err)
	}

	if len(owners) != 1 || owners[0].UserID != "user-b" {
		t.Fatalf("owner filter returned %v, want only user-b", owners)
	}

	search, err := repo.List(ctx, "tenant-a", ListOptions{Limit: 500, Search: strPtr("anna")})
	if err != nil {
		t.Fatal(err)
	}

	if len(search) != 1 || search[0].UserID != "user-a" {
		t.Fatalf("search filter returned %v, want only user-a", search)
	}

	missing, err := repo.List(ctx, "tenant-b", ListOptions{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}

	if len(missing) != 0 {
		t.Fatalf("other tenant returned %d members, want 0", len(missing))
	}
}

func TestLastOwnerGuard(t *testing.T) {
	repo, ctx := integrationMemberRepository(t)

	if err := repo.Sync(ctx, SyncParams{TenantID: "tenant-a", UserID: "user-a", Email: "a@example.com"}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.UpdateRole(ctx, "tenant-a", "user-a", RoleOwner); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.UpdateRole(ctx, "tenant-a", "user-a", RoleMember); err != nil {
		if !errors.Is(err, ErrLastOwner) {
			t.Fatalf("demote last owner: got %v, want %v", err, ErrLastOwner)
		}
	} else {
		t.Fatal("demote last owner: expected ErrLastOwner")
	}

	if err := repo.Delete(ctx, "tenant-a", "user-a"); err != nil {
		if !errors.Is(err, ErrLastOwner) {
			t.Fatalf("remove last owner: got %v, want %v", err, ErrLastOwner)
		}
	} else {
		t.Fatal("remove last owner: expected ErrLastOwner")
	}

	// With a second owner, demotion succeeds.
	if err := repo.Sync(ctx, SyncParams{TenantID: "tenant-a", UserID: "user-b", Email: "b@example.com"}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.UpdateRole(ctx, "tenant-a", "user-b", RoleOwner); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.UpdateRole(ctx, "tenant-a", "user-a", RoleMember); err != nil {
		t.Fatalf("demote one of two owners: %v", err)
	}

	if err := repo.Delete(ctx, "tenant-a", "user-a"); err != nil {
		t.Fatalf("remove non-owner: %v", err)
	}

	if _, err := repo.Get(ctx, "tenant-a", "user-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get removed member: got %v, want %v", err, ErrNotFound)
	}
}

func TestConcurrentLastOwnerDemotionLeavesOneOwner(t *testing.T) {
	repo, ctx := integrationMemberRepository(t)

	tenantID := "tenant-a"

	for _, userID := range []string{"user-a", "user-b"} {
		if err := repo.Sync(ctx, SyncParams{TenantID: tenantID, UserID: userID, Email: userID + "@example.com"}); err != nil {
			t.Fatal(err)
		}

		if _, err := repo.UpdateRole(ctx, tenantID, userID, RoleOwner); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup

	for i, userID := range []string{"user-a", "user-b"} {
		wg.Add(1)
		go func(i int, userID string) {
			defer wg.Done()
			<-start
			_, errs[i] = repo.UpdateRole(ctx, tenantID, userID, RoleMember)
		}(i, userID)
	}

	close(start)
	wg.Wait()

	rejections := 0
	for _, err := range errs {
		if errors.Is(err, ErrLastOwner) {
			rejections++
		} else if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if rejections != 1 {
		t.Fatalf("expected exactly one last-owner rejection, got %d (%v)", rejections, errs)
	}

	owners, err := repo.List(ctx, tenantID, ListOptions{Limit: 500, Role: rolePtr(RoleOwner)})
	if err != nil {
		t.Fatal(err)
	}

	if len(owners) != 1 {
		t.Fatalf("owner count = %d, want 1", len(owners))
	}
}

func rolePtr(role Role) *Role {
	return &role
}

func strPtr(value string) *string {
	return &value
}
