package task

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhdph/taskmanager/internal/project"
)

// sprintFilterFixture is a project with three tasks: one in a sprint, one
// in the backlog and one finished.
type sprintFilterFixture struct {
	tasks    *Repository
	projects *project.Repository
	pool     *pgxpool.Pool
	ctx      context.Context

	projectID string
	sprintID  string

	inSprint  string
	backlog   string
	completed string
}

func newSprintFilterFixture(
	t *testing.T,
) sprintFilterFixture {
	t.Helper()

	tasks, projects, pool, ctx := integrationTaskRepositoryWithPool(t)

	projectItem, err := projects.CreateWithDefaultStatuses(
		ctx,
		project.CreateParams{
			ID:              uuid.NewString(),
			TenantID:        "tenant-a",
			Name:            "sprint filters",
			CreatedByUserID: "manager",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	statuses, err := projects.ListStatuses(
		ctx,
		"tenant-a",
		projectItem.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	sprintID := uuid.NewString()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO sprints (
			id, tenant_id, project_id, name, planned_end_at,
			state, created_by_user_id
		 ) VALUES ($1, 'tenant-a', $2, 'Sprint', NOW() + INTERVAL '1 day', 'planned', 'manager')`,
		sprintID,
		projectItem.ID,
	); err != nil {
		t.Fatal(err)
	}

	create := func(
		title string,
		assignee string,
		completed bool,
	) Task {
		t.Helper()

		item, err := tasks.Create(ctx, CreateParams{
			ID:              uuid.NewString(),
			TenantID:        "tenant-a",
			ProjectID:       projectItem.ID,
			StatusID:        statuses[0].ID,
			Title:           title,
			Priority:        PriorityMedium,
			AssigneeUserID:  &assignee,
			CreatedByUserID: "manager",
		})
		if err != nil {
			t.Fatal(err)
		}

		if completed {
			if _, err := pool.Exec(
				ctx,
				`UPDATE tasks SET completed_at = NOW() WHERE id = $1`,
				item.ID,
			); err != nil {
				t.Fatal(err)
			}
		}

		return item
	}

	inSprint := create("in sprint", "manager", false)
	backlog := create("backlog", "manager", false)
	completed := create("completed", "manager", true)

	if _, err := pool.Exec(
		ctx,
		`UPDATE tasks SET sprint_id = $1 WHERE id = $2`,
		sprintID,
		inSprint.ID,
	); err != nil {
		t.Fatal(err)
	}

	return sprintFilterFixture{
		tasks:     tasks,
		projects:  projects,
		pool:      pool,
		ctx:       ctx,
		projectID: projectItem.ID,
		sprintID:  sprintID,
		inSprint:  inSprint.ID,
		backlog:   backlog.ID,
		completed: completed.ID,
	}
}

func TestTaskListingsFilterBySprintAndBacklog(t *testing.T) {
	fixture := newSprintFilterFixture(t)

	titles := func(items []Task) map[string]bool {
		names := make(map[string]bool, len(items))

		for _, item := range items {
			names[item.Title] = true
		}

		return names
	}

	// Project listing by sprint.
	items, err := fixture.tasks.ListForProject(
		fixture.ctx,
		"tenant-a",
		fixture.projectID,
		ListOptions{
			Limit:    50,
			SprintID: &fixture.sprintID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 || items[0].ID != fixture.inSprint {
		t.Fatalf("sprint filter = %+v", titles(items))
	}

	// Project listing by backlog: unfinished tasks without a sprint.
	items, err = fixture.tasks.ListForProject(
		fixture.ctx,
		"tenant-a",
		fixture.projectID,
		ListOptions{
			Limit:       50,
			BacklogOnly: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	names := titles(items)
	if len(items) != 1 || !names["backlog"] {
		t.Fatalf("backlog filter = %v", names)
	}

	// Workspace listing.
	workspaceItems, err := fixture.tasks.ListForTenant(
		fixture.ctx,
		"tenant-a",
		"manager",
		WorkspaceListOptions{
			Limit:    50,
			Sort:     WorkspaceSortCreated,
			SprintID: &fixture.sprintID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(workspaceItems) != 1 ||
		workspaceItems[0].Task.ID != fixture.inSprint {

		t.Fatalf("workspace sprint filter = %+v", workspaceItems)
	}

	workspaceItems, err = fixture.tasks.ListForTenant(
		fixture.ctx,
		"tenant-a",
		"manager",
		WorkspaceListOptions{
			Limit:       50,
			Sort:        WorkspaceSortCreated,
			BacklogOnly: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(workspaceItems) != 1 ||
		workspaceItems[0].Task.ID != fixture.backlog {

		t.Fatalf("workspace backlog filter = %+v", workspaceItems)
	}

	// My work listing.
	myWork, err := fixture.tasks.ListMyWork(
		fixture.ctx,
		"tenant-a",
		"manager",
		MyWorkOptions{
			Limit:    50,
			SprintID: &fixture.sprintID,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(myWork) != 1 || myWork[0].ID != fixture.inSprint {
		t.Fatalf("my work sprint filter = %+v", myWork)
	}

	myWork, err = fixture.tasks.ListMyWork(
		fixture.ctx,
		"tenant-a",
		"manager",
		MyWorkOptions{
			Limit:       50,
			BacklogOnly: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(myWork) != 1 || myWork[0].ID != fixture.backlog {
		t.Fatalf("my work backlog filter = %+v", myWork)
	}
}

func TestTaskRestoreLeavesCompletedSprint(t *testing.T) {
	fixture := newSprintFilterFixture(t)

	// The sprint is completed, so restoring the task returns it to the
	// backlog.
	if _, err := fixture.pool.Exec(
		fixture.ctx,
		`UPDATE sprints SET state = 'completed', completed_at = NOW()
		 WHERE id = $1`,
		fixture.sprintID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.pool.Exec(
		fixture.ctx,
		`UPDATE tasks SET archived_at = NOW() WHERE id = $1`,
		fixture.inSprint,
	); err != nil {
		t.Fatal(err)
	}

	restored, err := fixture.tasks.Restore(
		fixture.ctx,
		"tenant-a",
		fixture.projectID,
		fixture.inSprint,
		"manager",
	)
	if err != nil {
		t.Fatal(err)
	}

	if restored.SprintID != nil {
		t.Fatalf("restored task sprint = %v, want backlog", *restored.SprintID)
	}

	// The membership history records the release.
	var releases int
	if err := fixture.pool.QueryRow(
		fixture.ctx,
		`SELECT COUNT(*)
		 FROM sprint_task_membership
		 WHERE sprint_id = $1 AND task_id = $2 AND action = 'removed'`,
		fixture.sprintID,
		fixture.inSprint,
	).Scan(&releases); err != nil {
		t.Fatal(err)
	}

	if releases != 1 {
		t.Fatalf("release records = %d, want 1", releases)
	}
}

var _ = time.Now
