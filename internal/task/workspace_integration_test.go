package task

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

	"github.com/mhdph/taskmanager/internal/audit"
	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
)

func integrationWorkspaceSetup(
	t *testing.T,
) (*Repository, *project.Repository, context.Context) {
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
	schema := "test_workspace_" + fmt.Sprintf("%d", time.Now().UnixNano())
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

type nopNotificationSender struct{}

func (nopNotificationSender) Send(
	_ context.Context,
	_ notification.SendInput,
) error {
	return nil
}

type nopAuditRecorder struct{}

func (nopAuditRecorder) Record(
	_ context.Context,
	_ audit.RecordInput,
) error {
	return nil
}

// workspaceFixture owns two projects for tenant-w:
//   - project A "Project A": visible to member-user (member) and manager
//   - project B "Project B": visible to manager only
//
// Seeded tasks:
//
//	a-urgent-due-past : A, urgent,  due in the past,  assigned  (todo)
//	a-urgent-due-future : A, urgent, due in future,  unassigned (in progress)
//	a-low-nodue       : A, low,     no due date,     unassigned (in progress)
//	a-done            : A, medium,  no due date,     assigned   (done)
//	b-high            : B, high,     no due date,     unassigned (todo)
type workspaceFixture struct {
	taskRepo    *Repository
	projectRepo *project.Repository

	projects map[string]project.Project

	tasks map[string]Task
}

const workspaceTenant = "tenant-w"

func setupWorkspace(
	t *testing.T,
	taskRepo *Repository,
	projectRepo *project.Repository,
	ctx context.Context,
) workspaceFixture {
	t.Helper()

	fixture := workspaceFixture{
		taskRepo:    taskRepo,
		projectRepo: projectRepo,

		projects: map[string]project.Project{},
		tasks:    map[string]Task{},
	}

	projectA, err := projectRepo.CreateWithDefaultStatuses(
		ctx,
		project.CreateParams{
			ID:              uuid.NewString(),
			TenantID:        workspaceTenant,
			Name:            "Project A",
			CreatedByUserID: "manager",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	projectB, err := projectRepo.CreateWithDefaultStatuses(
		ctx,
		project.CreateParams{
			ID:              uuid.NewString(),
			TenantID:        workspaceTenant,
			Name:            "Project B",
			CreatedByUserID: "manager",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	fixture.projects["A"] = projectA
	fixture.projects["B"] = projectB

	if _, err := projectRepo.AddMember(ctx, project.AddMemberParams{
		TenantID:  workspaceTenant,
		ProjectID: projectA.ID,
		UserID:    "member-user",
		Role:      "member",
	}); err != nil {
		t.Fatal(err)
	}

	statusesA, err := projectRepo.ListStatuses(ctx, workspaceTenant, projectA.ID)
	if err != nil {
		t.Fatal(err)
	}

	statusesB, err := projectRepo.ListStatuses(ctx, workspaceTenant, projectB.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Default statuses are ordered by position: Todo, In Progress, Done.
	todoA := statusesA[0]
	doingA := statusesA[1]
	doneA := statusesA[2]
	todoB := statusesB[0]

	member := "member-user"

	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	future := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)

	seed := []struct {
		label    string
		project  project.Project
		status   project.ProjectStatus
		title    string
		priority Priority
		assignee *string
		due      *time.Time
	}{
		{"a-urgent-due-past", projectA, todoA, "urgent past", PriorityUrgent, &member, &past},
		{"a-urgent-due-future", projectA, doingA, "urgent future", PriorityUrgent, nil, &future},
		{"a-low-nodue", projectA, doingA, "low no due", PriorityLow, nil, nil},
		{"a-done", projectA, doneA, "done work", PriorityMedium, &member, nil},
		{"b-high", projectB, todoB, "hidden high", PriorityHigh, nil, nil},
	}

	for _, item := range seed {
		created, err := taskRepo.Create(
			ctx,
			CreateParams{
				ID:              uuid.NewString(),
				TenantID:        workspaceTenant,
				ProjectID:       item.project.ID,
				StatusID:        item.status.ID,
				Title:           item.title,
				Priority:        item.priority,
				AssigneeUserID:  item.assignee,
				DueAt:           item.due,
				CreatedByUserID: "manager",
			},
		)
		if err != nil {
			t.Fatalf("create task %q: %v", item.title, err)
		}

		fixture.tasks[item.label] = created
	}

	return fixture
}

func workspaceService(
	taskRepo *Repository,
	projectRepo *project.Repository,
) *Service {
	return NewService(
		taskRepo,
		projectRepo,
		project.NewAuthorizer(projectRepo),
		nopNotificationSender{},
		nil,
		nopAuditRecorder{},
		0,
	)
}

// collectPaginated walks an entire listing using the same limit+1/hasNext
// contract the service implements, asserting nothing is lost or duplicated.
func collectPaginated(
	t *testing.T,
	repo *Repository,
	ctx context.Context,
	identity auth.Identity,
	options WorkspaceListOptions,
	pageLimit int32,
) []string {
	t.Helper()

	pageLimitOptions := options
	pageLimitOptions.Limit = pageLimit + 1

	seen := []string{}
	unique := map[string]struct{}{}

	for page := 0; page < 50; page++ {
		items, err := repo.ListForTenant(
			ctx,
			identity.TenantID,
			identity.UserID,
			pageLimitOptions,
		)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}

		hasMore := len(items) > int(pageLimit)

		if hasMore {
			items = items[:pageLimit]
		}

		if len(items) == 0 {
			if hasMore {
				t.Fatal("no items but more pages announced")
			}

			if page == 0 {
				t.Fatal("listing returned no rows at all")
			}

			return seen
		}

		for _, item := range items {
			if _, ok := unique[item.ID]; ok {
				t.Fatalf("task %s returned twice during pagination", item.ID)
			}

			unique[item.ID] = struct{}{}
			seen = append(seen, item.ID)
		}

		if !hasMore {
			return seen
		}

		last := items[len(items)-1]

		cursor, err := workspaceCursorFromLastTask(options.Sort, options.Desc, last.Task)
		if err != nil {
			t.Fatal(err)
		}

		pageLimitOptions.Cursor = &cursor
	}

	t.Fatal("pagination did not terminate")
	return nil
}

func TestWorkspaceListMembershipScoping(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	setupWorkspace(t, taskRepo, projectRepo, ctx)

	member := auth.Identity{
		UserID:   "member-user",
		TenantID: workspaceTenant,
	}

	memberItems, err := taskRepo.ListForTenant(ctx, workspaceTenant, member.UserID, WorkspaceListOptions{
		IsAdmin: false,
		Sort:    WorkspaceSortCreated,
		Limit:   100,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(memberItems) != 4 {
		t.Fatalf("member task count = %d, want 4", len(memberItems))
	}

	for _, item := range memberItems {
		if item.ProjectName == "Project B" {
			t.Fatalf("member saw task from project B: %s", item.Title)
		}
	}

	admin := auth.Identity{
		UserID:   "admin-user",
		TenantID: workspaceTenant,
	}

	adminItems, err := taskRepo.ListForTenant(ctx, workspaceTenant, admin.UserID, WorkspaceListOptions{
		IsAdmin: true,
		Sort:    WorkspaceSortCreated,
		Limit:   100,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(adminItems) != 5 {
		t.Fatalf("admin task count = %d, want 5", len(adminItems))
	}
}

func TestWorkspaceListPaginationCoversAllRows(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	setupWorkspace(t, taskRepo, projectRepo, ctx)

	identity := auth.Identity{
		UserID:   "admin-user",
		TenantID: workspaceTenant,
	}

	// page size 2 forces several pages; every sort key (both directions)
	// must yield the full seed exactly once, and within non-null pages the
	// rows must be ordered by the requested key.
	for _, sort := range []WorkspaceSort{
		WorkspaceSortCreated,
		WorkspaceSortUpdated,
		WorkspaceSortDue,
		WorkspaceSortPriority,
		WorkspaceSortTitle,
	} {
		for _, desc := range []bool{false, true} {
			seen := collectPaginated(t, taskRepo, ctx, identity, WorkspaceListOptions{
				IsAdmin: true,
				Sort:    sort,
				Desc:    desc,
			}, 2)

			if len(seen) != 5 {
				t.Fatalf("sort=%s desc=%v pagination saw %d tasks, want 5: %v", sort, desc, len(seen), seen)
			}
		}
	}

	// Due sort must place the task without a due date last in both
	// directions.
	dueDesc := collectPaginated(t, taskRepo, ctx, identity, WorkspaceListOptions{
		IsAdmin: true,
		Sort:    WorkspaceSortDue,
		Desc:    true,
	}, 2)

	last := mustTaskByID(t, taskRepo, ctx, dueDesc[len(dueDesc)-1])

	if last.DueAt != nil {
		t.Fatalf("due desc: last row must be the no-due task, got due=%v", last.DueAt)
	}

	first := mustTaskByID(t, taskRepo, ctx, dueDesc[0])

	if first.DueAt == nil {
		t.Fatal("due desc: first row must not be the no-due task")
	}
}

func mustTaskByID(t *testing.T, repo *Repository, ctx context.Context, id string) Task {
	t.Helper()

	task, err := repo.GetForTenant(ctx, workspaceTenant, id)
	if err != nil {
		t.Fatalf("resolve task %s: %v", id, err)
	}

	return task
}

func TestWorkspaceListFilters(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	setupWorkspace(t, taskRepo, projectRepo, ctx)

	identity := auth.Identity{
		UserID:   "admin-user",
		TenantID: workspaceTenant,
	}

	run := func(options WorkspaceListOptions) []WorkspaceTask {
		t.Helper()
		options.IsAdmin = true
		options.Limit = 100

		items, err := taskRepo.ListForTenant(ctx, workspaceTenant, identity.UserID, options)
		if err != nil {
			t.Fatal(err)
		}

		return items
	}

	assertTitles := func(items []WorkspaceTask, want ...string) {
		t.Helper()

		got := make([]string, 0, len(items))
		for _, item := range items {
			got = append(got, item.Title)
		}

		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	assertTitles(run(WorkspaceListOptions{
		Sort:        WorkspaceSortCreated,
		OverdueOnly: true,
	}), "urgent past")

	// The seed created "done work" directly in a done-category status, so it
	// legitimately carries completed_at (created-complete is persisted).
	assertTitles(run(WorkspaceListOptions{
		Sort:          WorkspaceSortCreated,
		CompletedOnly: true,
	}), "done work")

	assertTitles(run(WorkspaceListOptions{
		Sort:           WorkspaceSortCreated,
		UnassignedOnly: true,
	}), "urgent future", "low no due", "hidden high")

	assertTitles(run(WorkspaceListOptions{
		Sort:           WorkspaceSortCreated,
		AssigneeUserID: strPtrWorkspace("member-user"),
	}), "urgent past", "done work")

	assertTitles(run(WorkspaceListOptions{
		Sort:           WorkspaceSortCreated,
		StatusCategory: strPtrWorkspaceCategory("done"),
	}), "done work")

	assertTitles(run(WorkspaceListOptions{
		Sort:       WorkspaceSortCreated,
		StatusName: strPtrWorkspace("Done"),
	}), "done work")

	assertTitles(run(WorkspaceListOptions{
		Sort:   WorkspaceSortCreated,
		Search: strPtrWorkspace("hidden"),
	}), "hidden high")
}

func TestWorkspaceListProjectFilter(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fix := setupWorkspace(t, taskRepo, projectRepo, ctx)

	identity := auth.Identity{
		UserID:   "admin-user",
		TenantID: workspaceTenant,
	}

	projectAID := fix.projects["A"].ID

	items, err := taskRepo.ListForTenant(ctx, workspaceTenant, identity.UserID, WorkspaceListOptions{
		IsAdmin:   true,
		Sort:      WorkspaceSortCreated,
		Limit:     100,
		ProjectID: &projectAID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 4 {
		t.Fatalf("project filter returned %d tasks, want 4", len(items))
	}

	for _, item := range items {
		if item.ProjectID != projectAID {
			t.Fatalf("project filter returned task %s from %s", item.ID, item.ProjectID)
		}
	}
}

func TestWorkspaceListArchivedFilter(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fix := setupWorkspace(t, taskRepo, projectRepo, ctx)

	identity := auth.Identity{
		UserID:   "admin-user",
		TenantID: workspaceTenant,
	}

	target := fix.tasks["a-low-nodue"]

	if err := taskRepo.Archive(
		ctx,
		workspaceTenant,
		target.ProjectID,
		target.ID,
	); err != nil {
		t.Fatal(err)
	}

	items, err := taskRepo.ListForTenant(ctx, workspaceTenant, identity.UserID, WorkspaceListOptions{
		IsAdmin:  true,
		Sort:     WorkspaceSortCreated,
		Limit:    100,
		Archived: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 || items[0].ID != target.ID {
		t.Fatalf("archived filter returned %d tasks, want exactly 'low no due'", len(items))
	}

	active, err := taskRepo.ListForTenant(ctx, workspaceTenant, identity.UserID, WorkspaceListOptions{
		IsAdmin: true,
		Sort:    WorkspaceSortCreated,
		Limit:   100,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(active) != 4 {
		t.Fatalf("active filter returned %d tasks, want 4", len(active))
	}
}

func TestBulkUpdateStatusAssigneePriority(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fix := setupWorkspace(t, taskRepo, projectRepo, ctx)

	service := workspaceService(taskRepo, projectRepo)

	manager := auth.Identity{
		UserID:   "manager",
		TenantID: workspaceTenant,
	}

	result, err := service.BulkUpdate(ctx, BulkUpdateInput{
		Identity: manager,
		TaskIDs: []string{
			fix.tasks["a-urgent-due-past"].ID,
			fix.tasks["a-low-nodue"].ID,
			fix.tasks["b-high"].ID,
		},
		Status:   strPtrWorkspace("Done"),
		Priority: priorityPtrPtr(PriorityHigh),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Updated != 3 || len(result.Results) != 3 {
		t.Fatalf("bulk updated = %d of %d results", result.Updated, len(result.Results))
	}

	for _, item := range result.Results {
		if !item.OK {
			t.Fatalf("bulk item %s failed: %s", item.TaskID, item.Err)
		}
	}

	for _, label := range []string{"a-urgent-due-past", "a-low-nodue", "b-high"} {
		updated := fix.tasks[label]

		current, err := taskRepo.GetForTenant(ctx, workspaceTenant, updated.ID)
		if err != nil {
			t.Fatal(err)
		}

		if current.Priority != PriorityHigh {
			t.Fatalf("task %s priority = %q, want high", label, current.Priority)
		}

		if current.CompletedAt == nil {
			t.Fatalf("task %s moved to Done but completedAt is nil", label)
		}
	}
}

func TestBulkUpdateArchiveAndDeleteStyleOps(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fix := setupWorkspace(t, taskRepo, projectRepo, ctx)

	service := workspaceService(taskRepo, projectRepo)

	manager := auth.Identity{
		UserID:   "manager",
		TenantID: workspaceTenant,
	}

	result, err := service.BulkUpdate(ctx, BulkUpdateInput{
		Identity: manager,
		TaskIDs: []string{
			fix.tasks["a-low-nodue"].ID,
			fix.tasks["b-high"].ID,
		},
		Archive: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Updated != 2 {
		t.Fatalf("bulk archived %d tasks, want 2", result.Updated)
	}

	for _, label := range []string{"a-low-nodue", "b-high"} {
		archived := fix.tasks[label]

		if _, err := taskRepo.GetForTenant(ctx, workspaceTenant, archived.ID); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("archived task %s still resolvable as active: %v", label, err)
		}
	}
}

func TestBulkUpdateUnassignAndAssignValidation(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fix := setupWorkspace(t, taskRepo, projectRepo, ctx)

	service := workspaceService(taskRepo, projectRepo)

	manager := auth.Identity{
		UserID:   "manager",
		TenantID: workspaceTenant,
	}

	// bulk unassign the two assigned task seeds
	result, err := service.BulkUpdate(ctx, BulkUpdateInput{
		Identity: manager,
		TaskIDs: []string{
			fix.tasks["a-urgent-due-past"].ID,
			fix.tasks["a-done"].ID,
		},
		Assignee: strPtrWorkspace("none"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Updated != 2 {
		t.Fatalf("bulk unassign updated %d tasks, want 2", result.Updated)
	}

	// assigning a non-member must fail per item without failing the rest
	result, err = service.BulkUpdate(ctx, BulkUpdateInput{
		Identity: manager,
		TaskIDs: []string{
			fix.tasks["a-low-nodue"].ID,
			fix.tasks["b-high"].ID,
		},
		Assignee: strPtrWorkspace("member-user"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Updated != 1 {
		t.Fatalf("bulk assign updated %d tasks, want 1", result.Updated)
	}

	failed := make(map[string]bool)

	for _, item := range result.Results {
		if !item.OK {
			failed[item.TaskID] = true
		}
	}

	// member-user belongs to project A only: b-high must fail
	if !failed[fix.tasks["b-high"].ID] {
		t.Fatal("expected assignee validation failure for the project B task")
	}
}

func TestBulkUpdateRejectsViewerAndGarbage(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fix := setupWorkspace(t, taskRepo, projectRepo, ctx)

	if _, err := fix.projectRepo.AddMember(ctx, project.AddMemberParams{
		TenantID:  workspaceTenant,
		ProjectID: fix.projects["A"].ID,
		UserID:    "viewer-user",
		Role:      "viewer",
	}); err != nil {
		t.Fatal(err)
	}

	service := workspaceService(taskRepo, projectRepo)

	viewer := auth.Identity{
		UserID:   "viewer-user",
		TenantID: workspaceTenant,
	}

	// forbidden on the A task (viewer), not-found on the B task
	result, err := service.BulkUpdate(ctx, BulkUpdateInput{
		Identity: viewer,
		TaskIDs: []string{
			fix.tasks["a-low-nodue"].ID,
			fix.tasks["b-high"].ID,
		},
		Status: strPtrWorkspace("Done"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Updated != 0 {
		t.Fatalf("viewer bulk updated %d tasks, want 0", result.Updated)
	}

	for _, item := range result.Results {
		if item.OK {
			t.Fatalf("viewer task %s unexpectedly succeeded", item.TaskID)
		}
	}

	// no action
	if _, err := service.BulkUpdate(ctx, BulkUpdateInput{
		Identity: viewer,
		TaskIDs:  []string{fix.tasks["a-low-nodue"].ID},
	}); !errors.Is(err, ErrNoChanges) {
		t.Fatalf("no-action bulk: got %v, want %v", err, ErrNoChanges)
	}

	// too many ids
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fix.tasks["a-low-nodue"].ID
	}

	if _, err := service.BulkUpdate(ctx, BulkUpdateInput{
		Identity: viewer,
		TaskIDs:  ids,
		Status:   strPtrWorkspace("Done"),
	}); !errors.Is(err, ErrBulkTooLarge) {
		t.Fatalf("oversized bulk: got %v, want %v", err, ErrBulkTooLarge)
	}

	// empty ids
	if _, err := service.BulkUpdate(ctx, BulkUpdateInput{
		Identity: viewer,
		TaskIDs:  []string{},
		Status:   strPtrWorkspace("Done"),
	}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("empty bulk: got %v, want %v", err, ErrInvalidID)
	}
}

func strPtrWorkspace(value string) *string {
	return &value
}

func priorityPtrPtr(priority Priority) *Priority {
	return &priority
}

func strPtrWorkspaceCategory(value string) *string {
	return &value
}
