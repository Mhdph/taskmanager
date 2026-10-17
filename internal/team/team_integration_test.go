package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/member"
	"github.com/mhdph/taskmanager/internal/project"
)

// integrationSetup bootstraps a throwaway schema + migrations, mirroring
// the other integration fixtures.
func integrationSetup(
	t *testing.T,
) (*pgxpool.Pool, *Repository, *Service, context.Context) {
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

	schema := "test_team_" + strings.ReplaceAll(
		uuid.NewString(),
		"-",
		"",
	)

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(admin.Close)

	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
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

	repository := NewRepository(pool, queries)

	service := NewService(
		repository,
		member.NewRepository(pool, queries),
	)

	return pool, repository, service, ctx
}

func seedUsers(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	tenant string,
	users []string,
) {
	t.Helper()

	for _, user := range users {
		if _, err := pool.Exec(ctx, `INSERT INTO tenant_members (tenant_id, user_id) VALUES ($1, $2)`, tenant, user); err != nil {
			t.Fatal(err)
		}
	}
}

func adminIdentity() auth.Identity {
	return auth.Identity{
		UserID:   "admin-user",
		TenantID: "tenant-teams",
		Role:     auth.RoleAdmin,
	}
}

func memberIdentity() auth.Identity {
	return auth.Identity{
		UserID:   "plain-user",
		TenantID: "tenant-teams",
		Role:     auth.RoleMember,
	}
}

func TestTeamLifecycle(t *testing.T) {
	pool, _, service, ctx := integrationSetup(t)

	const tenant = "tenant-teams"

	seedUsers(t, pool, ctx, tenant, []string{"u1", "u2", "u3"})

	admin := adminIdentity()
	plain := memberIdentity()

	// Only admins can create.
	if _, err := service.Create(ctx, CreateInput{
		Identity: plain,

		Name: "Nope",
	}); !errors.Is(err, ErrForbidden) {

		t.Fatalf("member create err = %v, want ErrForbidden", err)
	}

	created, err := service.Create(ctx, CreateInput{
		Identity: admin,

		Name: "Design",

		Icon:  "palette",
		Color: "#8662C9",

		Description: "Product design, brand, and research",
	})
	if err != nil {
		t.Fatal(err)
	}

	if created.Name != "Design" || created.Icon != "palette" || created.Description != "Product design, brand, and research" {
		t.Fatalf("created team wrong: %+v", created)
	}

	// Duplicate names conflict within the tenant.
	if _, err := service.Create(ctx, CreateInput{
		Identity: admin,

		Name: "Design",
	}); !errors.Is(err, ErrTeamConflict) {

		t.Fatalf("duplicate name err = %v, want ErrTeamConflict", err)
	}

	// Validation.
	if _, err := service.Create(ctx, CreateInput{
		Identity: admin,
	}); !errors.Is(err, ErrTeamNameRequired) {

		t.Fatalf("missing name err = %v", err)
	}

	if _, err := service.Create(ctx, CreateInput{
		Identity: admin,

		Name: strings.Repeat("x", maxNameLength+1),
	}); !errors.Is(err, ErrTeamNameTooLong) {

		t.Fatalf("long name err = %v", err)
	}

	// A second team.
	second, err := service.Create(ctx, CreateInput{
		Identity: admin,

		Name: "Growth",
	})
	if err != nil {
		t.Fatal(err)
	}

	if second.Icon != "users" {
		t.Fatalf("icon default = %q, want users", second.Icon)
	}

	// List is name-ordered.
	teams, err := service.List(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}

	if len(teams) != 2 || teams[0].Name != "Design" || teams[1].Name != "Growth" {
		t.Fatalf("list order wrong: %+v", teams)
	}

	// Rename and edit.
	renamed, err := service.Update(ctx, UpdateInput{
		Identity: admin,

		TeamID: created.ID,

		Name: ptr("Design & Brand"),
		Icon: ptr("brush"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if renamed.Name != "Design & Brand" ||
		renamed.Icon != "brush" ||
		renamed.Color != "#8662C9" {

		t.Fatalf("rename lost data: %+v", renamed)
	}

	// Update with nothing supplied.
	if _, err := service.Update(ctx, UpdateInput{
		Identity: admin,

		TeamID: second.ID,
	}); !errors.Is(err, ErrNothingToUpdate) {

		t.Fatalf("no-op update err = %v", err)
	}

	// Membership: invalid user ids are refused.
	if err := service.SetMembers(ctx, SetMembersInput{
		Identity: admin,

		TeamID: created.ID,

		UserIDs: []string{"u1", "ghost"},
	}); !errors.Is(err, ErrTeamMembersInvalid) {

		t.Fatalf("ghost member err = %v", err)
	}

	// Assign two members.
	if err := service.SetMembers(ctx, SetMembersInput{
		Identity: admin,

		TeamID: created.ID,

		UserIDs: []string{"u2", "u1", "u1"},
	}); err != nil {

		t.Fatal(err)
	}

	ids, err := service.ListMembers(ctx, ListMembersInput{
		Identity: admin,

		TeamID: created.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(ids, []string{"u1", "u2"}) {
		t.Fatalf("member ids = %v", ids)
	}

	// Replacing the list moves people: u1 leaves to Growth.
	if err := service.SetMembers(ctx, SetMembersInput{
		Identity: admin,

		TeamID: second.ID,

		UserIDs: []string{"u1"},
	}); err != nil {

		t.Fatal(err)
	}

	ids, err = service.ListMembers(ctx, ListMembersInput{
		Identity: admin,

		TeamID: second.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(ids, []string{"u1"}) {
		t.Fatalf("growth member ids = %v", ids)
	}

	ids, err = service.ListMembers(ctx, ListMembersInput{
		Identity: admin,

		TeamID: created.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(ids, []string{"u2"}) {
		t.Fatalf("design member ids after move = %v", ids)
	}

	// Non-admins cannot manage.
	if err := service.SetMembers(ctx, SetMembersInput{
		Identity: plain,

		TeamID: created.ID,

		UserIDs: []string{"u3"},
	}); !errors.Is(err, ErrForbidden) {

		t.Fatalf("member set err = %v, want ErrForbidden", err)
	}

	// Delete the moving team; membership drops to nobody.
	if err := service.Delete(ctx, admin, second.ID); err != nil {
		t.Fatal(err)
	}

	if err := service.Delete(ctx, admin, second.ID); !errors.Is(err, ErrTeamNotFound) {

		t.Fatalf("double delete err = %v", err)
	}

	ids, err = service.ListMembers(ctx, ListMembersInput{
		Identity: admin,

		TeamID: created.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(ids) != 1 {
		t.Fatalf("design member ids = %v, want u2 only", ids)
	}
}

func TestProjectTeamLink(t *testing.T) {
	pool, _, service, ctx := integrationSetup(t)

	const tenant = "tenant-teams-link"

	seedUsers(t, pool, ctx, tenant, []string{"owner"})

	admin := adminIdentity()
	admin.TenantID = tenant

	team, err := service.Create(ctx, CreateInput{
		Identity: admin,

		Name: "Design",
	})
	if err != nil {
		t.Fatal(err)
	}

	projectRepo := project.NewRepository(pool, db.New(pool))

	item, err := projectRepo.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		Name:            "Design system",
		CreatedByUserID: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Attach the team.
	updated, err := projectRepo.Update(ctx, project.UpdateParams{
		TenantID:  tenant,
		ProjectID: item.ID,

		UpdateTeam: true,
		TeamID:     &team.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if updated.TeamID == nil || *updated.TeamID != team.ID {
		t.Fatalf("project team = %v, want %s", updated.TeamID, team.ID)
	}

	// Unknown team id is refused outright (not silently cleared).
	if _, err := projectRepo.Update(ctx, project.UpdateParams{
		TenantID:  tenant,
		ProjectID: item.ID,

		UpdateTeam: true,
		TeamID:     ptr(uuid.NewString()),
	}); !errors.Is(err, project.ErrTeamNotFound) {

		t.Fatalf("unknown team err = %v, want ErrTeamNotFound", err)
	}

	// Deleting the team unassigns the project instead of failing.
	if err := service.Delete(ctx, admin, team.ID); err != nil {
		t.Fatal(err)
	}

	refreshed, err := projectRepo.GetForTenant(ctx, tenant, item.ID)
	if err != nil {
		t.Fatal(err)
	}

	if refreshed.TeamID != nil {
		t.Fatalf("project team after team delete = %v, want nil", refreshed.TeamID)
	}
}

func ptr(value string) *string {
	return &value
}
