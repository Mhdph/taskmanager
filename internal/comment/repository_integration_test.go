package comment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

type noopNotificationSender struct{}

func (noopNotificationSender) Send(context.Context, notification.SendInput) error { return nil }

func integrationCommentRepositories(t *testing.T) (*Repository, *task.Repository, *project.Repository, context.Context) {
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
	return NewRepository(pool, queries), task.NewRepository(pool, queries), project.NewRepository(pool, queries), ctx
}

func TestCommentsBatchMentionsAndRejectNonMember(t *testing.T) {
	comments, tasks, projects, ctx := integrationCommentRepositories(t)
	const tenant, manager, member = "tenant-a", "manager", "member"
	projectItem, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: tenant, Name: "comments", CreatedByUserID: manager})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.AddMember(ctx, project.AddMemberParams{TenantID: tenant, ProjectID: projectItem.ID, UserID: member, Role: project.MemberRoleMember}); err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, tenant, projectItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	taskItem, err := tasks.Create(ctx, task.CreateParams{ID: uuid.NewString(), TenantID: tenant, ProjectID: projectItem.ID, StatusID: statuses[0].ID, Title: "task", Priority: task.PriorityMedium, CreatedByUserID: manager})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(comments, tasks, projects, project.NewAuthorizer(projects), noopNotificationSender{})
	identity := auth.Identity{TenantID: tenant, UserID: manager}
	for i := 0; i < 3; i++ {
		if _, err := service.Create(ctx, CreateInput{Identity: identity, ProjectID: projectItem.ID, TaskID: taskItem.ID, Body: "hello", MentionedUserIDs: []string{member}}); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := comments.ListForTask(ctx, tenant, projectItem.ID, taskItem.ID, ListOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("listed %d comments, want 3", len(listed))
	}
	for _, item := range listed {
		if len(item.MentionedUserIDs) != 1 || item.MentionedUserIDs[0] != member {
			t.Fatalf("mentions for comment %s = %v", item.ID, item.MentionedUserIDs)
		}
	}
	_, err = service.Create(ctx, CreateInput{Identity: identity, ProjectID: projectItem.ID, TaskID: taskItem.ID, Body: "invalid mention", MentionedUserIDs: []string{"not-a-member"}})
	if !errors.Is(err, ErrMentionedUserNotMember) {
		t.Fatalf("nonmember mention error = %v", err)
	}
}
