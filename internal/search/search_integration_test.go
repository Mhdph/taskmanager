package search

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
) (*pgxpool.Pool, *Repository, *Service, *project.Repository, *task.Repository, context.Context) {
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

	schema := "test_search_" + fmt.Sprintf("%d", time.Now().UnixNano())

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

	taskRepo := task.NewRepository(pool, queries)

	return pool, repo, NewService(repo), projectRepo, taskRepo, ctx
}

func seedCommentInsert(
	t *testing.T,
	pool *pgxpool.Pool,
	tenantID string,
	projectID string,
	taskID uuid.UUID,
	author string,
	body string,
	deleted bool,
) uuid.UUID {
	t.Helper()

	id := uuid.New()

	deletedAt := "NULL"
	if deleted {
		deletedAt = "NOW()"
	}

	query := fmt.Sprintf(
		`INSERT INTO task_comments (
			id, tenant_id, project_id, task_id, author_user_id, body, deleted_at
		) VALUES ($1, $2, $3, $4, $5, $6, %s)`,
		deletedAt,
	)

	if _, err := pool.Exec(
		context.Background(),
		query,
		id, tenantID, projectID, taskID, author, body,
	); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	return id
}

func seedAttachmentInsert(
	t *testing.T,
	pool *pgxpool.Pool,
	tenantID string,
	projectID string,
	taskID uuid.UUID,
	filename string,
	deleted bool,
) {
	t.Helper()

	deletedAt := "NULL"
	if deleted {
		deletedAt = "NOW()"
	}

	query := fmt.Sprintf(
		`INSERT INTO task_attachments (
			id, tenant_id, project_id, task_id, uploaded_by_user_id,
			filename, content_type, size_bytes, object_key, deleted_at
		) VALUES ($1, $2, $3, $4, 'manager', $5, 'text/plain', 12, $6, %s)`,
		deletedAt,
	)

	objectKey := "uploads/" + uuid.NewString()

	if _, err := pool.Exec(
		context.Background(),
		query,
		uuid.New(), tenantID, projectID, taskID, filename, objectKey,
	); err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
}

func seedTenantMember(
	t *testing.T,
	pool *pgxpool.Pool,
	tenantID string,
	userID string,
	email string,
	first string,
	last string,
	title string,
) {
	t.Helper()

	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO tenant_members (
			tenant_id, user_id, email, first_name, last_name, role, title
		) VALUES ($1, $2, $3, $4, $5, 'member', $6)`,
		tenantID, userID, email, first, last, title,
	); err != nil {
		t.Fatalf("seed tenant member: %v", err)
	}
}

func seedSearchFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	projectRepo *project.Repository,
	taskRepo *task.Repository,
	ctx context.Context,
) (projectA project.Project, projectB project.Project, taskA task.Task, taskB uuid.UUID) {
	t.Helper()

	const tenant = "tenant-search"

	description := "alpha project"

	projectA, err := projectRepo.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		Name:            "Searchable Alpha",
		Description:     &description,
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

	betaDescription := "beta project"

	projectB, err = projectRepo.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		Name:            "Beta",
		Description:     &betaDescription,
		CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}

	statuses, err := projectRepo.ListStatuses(ctx, tenant, projectA.ID)
	if err != nil {
		t.Fatal(err)
	}

	taskA, err = taskRepo.Create(ctx, task.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		ProjectID:       projectA.ID,
		StatusID:        statuses[0].ID,
		Title:           "wireframe alpha task",
		Priority:        task.PriorityMedium,
		CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}

	statusesB, err := projectRepo.ListStatuses(ctx, tenant, projectB.ID)
	if err != nil {
		t.Fatal(err)
	}

	createdB, err := taskRepo.Create(ctx, task.CreateParams{
		ID:              uuid.NewString(),
		TenantID:        tenant,
		ProjectID:       projectB.ID,
		StatusID:        statusesB[0].ID,
		Title:           "beta task",
		Priority:        task.PriorityMedium,
		CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}

	return projectA, projectB, taskA, uuid.MustParse(createdB.ID)
}

func TestSearchPeopleFilesComments(t *testing.T) {
	pool, repo, service, projectRepo, taskRepo, ctx := integrationSetup(t)

	const tenant = "tenant-search"

	projectA, projectB, taskA, taskB := seedSearchFixture(t, pool, projectRepo, taskRepo, ctx)

	seedTenantMember(t, pool, tenant, "member-user", "member@example.com", "Mina", "Member", "Designer")
	seedTenantMember(t, pool, tenant, "architect", "arch@gr8r.io", "Omar", "Architect", "Lead architect")

	// Comments: two matches in project A, one in project B, one deleted.
	taskAID := uuid.MustParse(taskA.ID)

	seedCommentInsert(t, pool, tenant, projectA.ID, taskAID, "member-user", "mention wireframes please", false)
	seedCommentInsert(t, pool, tenant, projectA.ID, taskAID, "manager", "wireframes are done", false)
	seedCommentInsert(t, pool, tenant, projectA.ID, taskAID, "manager", "deleted wireframes note", true)
	seedCommentInsert(t, pool, tenant, projectB.ID, taskB, "manager", "beta wireframes chat", false)

	// Attachments: two matches in project A, one deleted, one in project B.
	seedAttachmentInsert(t, pool, tenant, projectA.ID, taskAID, "wireframes-v1.fig", false)
	seedAttachmentInsert(t, pool, tenant, projectA.ID, taskAID, "wireframes-final.fig", true)
	seedAttachmentInsert(t, pool, tenant, projectB.ID, taskB, "beta-wireframes.fig", false)

	member := auth.Identity{
		UserID:   "member-user",
		TenantID: tenant,
	}

	admin := auth.Identity{
		UserID:   "admin-user",
		TenantID: tenant,
		Role:     auth.RoleAdmin,
	}

	// People: directory-wide, matched by name, email or title.
	people, err := repo.Members(ctx, tenant, "mina", 20)
	if err != nil {
		t.Fatal(err)
	}

	if len(people) != 1 || people[0].ID != "member-user" {
		t.Fatalf("people matches = %+v", people)
	}

	if people[0].Type != ResultTypePerson ||
		people[0].Description == nil ||
		*people[0].Description != "member@example.com" {

		t.Fatalf("person mapping wrong: %+v", people[0])
	}

	if byEmail, err := repo.Members(ctx, tenant, "arch@gr8r.io", 20); err != nil || len(byEmail) != 1 {
		t.Fatalf("email match failed: %v", err)
	}

	if byTitle, err := repo.Members(ctx, tenant, "Lead Architect", 20); err != nil || len(byTitle) != 1 {
		t.Fatalf("title match failed: %v", err)
	}

	// Comments: membership scoped.
	memberComments, err := service.Search(ctx, Input{
		Identity: member,
		Query:    "wireframes",
		Type:     "comments",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(memberComments.Comments) != 2 {
		t.Fatalf("member comments = %+v, want 2", memberComments.Comments)
	}

	for _, item := range memberComments.Comments {
		if item.TaskID == nil || *item.TaskID != taskA.ID {
			t.Fatalf("comment task id wrong: %+v", item)
		}
	}

	adminComments, err := service.Search(ctx, Input{
		Identity: admin,
		Query:    "wireframes",
		Type:     "comments",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(adminComments.Comments) != 3 {
		t.Fatalf("admin comments = %+v, want 3 (deleted excluded)", adminComments.Comments)
	}

	// Files: membership scoped, deleted filtered out.
	memberFiles, err := service.Search(ctx, Input{
		Identity: member,
		Query:    "wireframes",
		Type:     "files",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(memberFiles.Files) != 1 || memberFiles.Files[0].Title != "wireframes-v1.fig" {
		t.Fatalf("member files = %+v", memberFiles.Files)
	}

	adminFiles, err := service.Search(ctx, Input{
		Identity: admin,
		Query:    "wireframes",
		Type:     "files",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(adminFiles.Files) != 2 {
		t.Fatalf("admin files = %+v, want 2", adminFiles.Files)
	}

	if adminFiles.Files[0].TaskID == nil {
		t.Fatalf("file missing task id: %+v", adminFiles.Files[0])
	}
}

func TestSearchTypeRoutingAndValidation(t *testing.T) {
	_, _, service, _, _, ctx := integrationSetup(t)

	const tenant = "tenant-search"

	admin := auth.Identity{
		UserID:   "admin-user",
		TenantID: tenant,
		Role:     auth.RoleAdmin,
	}

	// type=people never touches comments/files.
	result, err := service.Search(ctx, Input{
		Identity: admin,
		Query:    "anything",
		Type:     "people",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.People) != 0 || len(result.Comments) != 0 || len(result.Files) != 0 || len(result.Tasks) != 0 {
		t.Fatalf("unexpected results in scoped search: %+v", result)
	}

	if _, err := service.Search(ctx, Input{
		Identity: admin,
		Query:    "a",
	}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("short query err = %v, want ErrInvalidQuery", err)
	}

	if _, err := service.Search(ctx, Input{
		Identity: admin,
		Query:    "valid",
		Type:     "spaceships",
	}); !errors.Is(err, ErrInvalidType) {
		t.Fatalf("bad type err = %v, want ErrInvalidType", err)
	}
}
