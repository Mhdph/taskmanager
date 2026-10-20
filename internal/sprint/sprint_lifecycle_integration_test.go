package sprint

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/project"
)

func TestSprintLifecycleAndFrozenReport(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	statusID := insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")
	insertSprintMember(t, pool, ctx, projectID, "bob", "member")

	alice := sprintIdentity("alice")
	bob := sprintIdentity("bob")

	first := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"first", estimate(3600), false,
	)
	second := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"second", estimate(1800), false,
	)
	third := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"third", nil, false,
	)

	end := time.Now().Add(48 * time.Hour)

	sprint, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Sprint 1",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	if sprint.State != StatePlanned {
		t.Fatalf("state = %q, want planned", sprint.State)
	}

	// A member may plan work into the sprint.
	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    first,

		SprintID: &sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    second,

		SprintID: &sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// Starting snapshots the commitment.
	started, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if started.State != StateActive || started.StartedAt == nil {
		t.Fatalf("started sprint = %+v", started)
	}

	// Repeating the start is idempotent.
	again, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.State != StateActive {
		t.Fatalf("repeated start state = %q", again.State)
	}

	// The first task is finished while the sprint runs.
	if _, err := pool.Exec(
		ctx,
		`UPDATE tasks SET completed_at = NOW() WHERE id = $1`,
		first,
	); err != nil {
		t.Fatal(err)
	}

	// Scope changes: one task is added, one is dropped.
	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    third,

		SprintID: &sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    second,

		SprintID: nil,
	}); err != nil {
		t.Fatal(err)
	}

	live, err := service.Report(ctx, ReportInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if live.Frozen {
		t.Fatal("live report is frozen")
	}
	if live.CommitmentTaskCount != 2 {
		t.Fatalf("commitment = %d, want 2", live.CommitmentTaskCount)
	}
	if live.CommitmentEstimatedSeconds != 5400 {
		t.Fatalf(
			"commitment seconds = %d, want 5400",
			live.CommitmentEstimatedSeconds,
		)
	}
	if live.AddedTaskCount != 1 {
		t.Fatalf("added = %d, want 1", live.AddedTaskCount)
	}
	if live.RemovedTaskCount != 1 {
		t.Fatalf("removed = %d, want 1", live.RemovedTaskCount)
	}
	if live.CompletedTaskCount != 1 {
		t.Fatalf("completed = %d, want 1", live.CompletedTaskCount)
	}
	if live.CarryoverTaskCount != 1 {
		t.Fatalf("carryover = %d, want 1", live.CarryoverTaskCount)
	}
	if live.UnestimatedTaskCount != 1 {
		t.Fatalf("unestimated = %d, want 1", live.UnestimatedTaskCount)
	}

	// Completing freezes the report and returns the unfinished work to the
	// backlog.
	completed, report, err := service.Complete(ctx, CompleteInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if completed.State != StateCompleted {
		t.Fatalf("completed state = %q", completed.State)
	}

	if !report.Frozen {
		t.Fatal("completion report is not frozen")
	}
	if report.CarryoverTaskCount != 1 {
		t.Fatalf("carryover = %d, want 1", report.CarryoverTaskCount)
	}

	// The finished task keeps the completed sprint, the unfinished task
	// goes back to the backlog.
	var firstSprint any
	var thirdSprint any

	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id FROM tasks WHERE id = $1`,
		first,
	).Scan(&firstSprint); err != nil {
		t.Fatal(err)
	}

	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id FROM tasks WHERE id = $1`,
		third,
	).Scan(&thirdSprint); err != nil {
		t.Fatal(err)
	}

	if firstSprint == nil {
		t.Fatal("finished task lost its sprint assignment")
	}
	if thirdSprint != nil {
		t.Fatal("unfinished task was not returned to the backlog")
	}

	// Later edits cannot rewrite the frozen report.
	if _, err := pool.Exec(
		ctx,
		`UPDATE tasks SET title = 'renamed', estimate_seconds = 9999 WHERE id = $1`,
		first,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`UPDATE tasks SET completed_at = NOW() WHERE id = $1`,
		third,
	); err != nil {
		t.Fatal(err)
	}

	// Later sprint participation cannot rewrite the report either.
	later, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Later sprint",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The task that left the sprint before the completion is unfinished
	// and in the backlog, so it can join the later sprint.
	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: alice,

		ProjectID: projectID,
		TaskID:    second,

		SprintID: &later.ID,
	}); err != nil {
		t.Fatal(err)
	}

	after, err := service.Report(ctx, ReportInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !after.Frozen {
		t.Fatal("report is no longer frozen")
	}
	if after.CompletedTaskCount != report.CompletedTaskCount ||
		after.CarryoverTaskCount != report.CarryoverTaskCount ||
		after.CommitmentTaskCount != report.CommitmentTaskCount {

		t.Fatalf("report changed after edits: %+v vs %+v", after, report)
	}

	// Completing again returns the same report and carries nothing over a
	// second time.
	_, repeated, err := service.Complete(ctx, CompleteInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.CarryoverTaskCount != report.CarryoverTaskCount {
		t.Fatalf("repeated completion changed the report: %+v", repeated)
	}
}

func TestSprintAssignmentRules(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	statusID := insertSprintProject(t, pool, ctx, projectID)

	otherProjectID := uuid.NewString()
	insertSprintProject(t, pool, ctx, otherProjectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")
	insertSprintMember(t, pool, ctx, projectID, "bob", "member")
	insertSprintMember(t, pool, ctx, projectID, "carol", "viewer")
	insertSprintMember(t, pool, ctx, otherProjectID, "alice", "manager")

	alice := sprintIdentity("alice")
	bob := sprintIdentity("bob")
	carol := sprintIdentity("carol")

	end := time.Now().Add(24 * time.Hour)

	sprint, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Planning",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	otherSprint, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: otherProjectID,

		Name: "Other project",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"task", nil, false,
	)
	child := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"child", nil, false,
	)
	completedTask := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"done", nil, true,
	)
	archivedTask := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"archived", nil, false,
	)

	if _, err := pool.Exec(
		ctx,
		`UPDATE tasks SET parent_task_id = $1, archived_at = NOW() WHERE id = $2`,
		task,
		child,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`UPDATE tasks SET archived_at = NOW() WHERE id = $1`,
		archivedTask,
	); err != nil {
		t.Fatal(err)
	}

	// A viewer cannot change the plan.
	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: carol,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &sprint.ID,
	}); !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("viewer assignment error = %v, want forbidden", err)
	}

	// A member can.
	assigned, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &sprint.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if assigned.SprintID == nil || *assigned.SprintID != sprint.ID {
		t.Fatalf("assignment = %+v, want sprint %s", assigned, sprint.ID)
	}

	// Moving the parent leaves the child alone.
	var childSprint any
	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id FROM tasks WHERE id = $1`,
		child,
	).Scan(&childSprint); err != nil {
		t.Fatal(err)
	}
	if childSprint != nil {
		t.Fatal("subtask followed its parent into the sprint")
	}

	// A sprint of another project cannot be used.
	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &otherSprint.ID,
	}); !errors.Is(err, ErrSprintNotFound) {
		t.Fatalf("cross-project assignment error = %v", err)
	}

	// Finished and archived tasks keep their plan.
	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    completedTask,

		SprintID: &sprint.ID,
	}); !errors.Is(err, ErrTaskNotEligible) {
		t.Fatalf("completed task error = %v, want ErrTaskNotEligible", err)
	}

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    archivedTask,

		SprintID: &sprint.ID,
	}); !errors.Is(err, ErrTaskNotEligible) {
		t.Fatalf("archived task error = %v, want ErrTaskNotEligible", err)
	}

	// Returning a task to the backlog clears the assignment.
	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: bob,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: nil,
	}); err != nil {
		t.Fatal(err)
	}

	var sprintID any
	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id FROM tasks WHERE id = $1`,
		task,
	).Scan(&sprintID); err != nil {
		t.Fatal(err)
	}
	if sprintID != nil {
		t.Fatal("task was not returned to the backlog")
	}
}

func TestSprintCarryoverToPlannedSprint(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	statusID := insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")

	alice := sprintIdentity("alice")

	end := time.Now().Add(24 * time.Hour)

	active, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Running",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	next, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Next",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"carry me", estimate(600), false,
	)

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: alice,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &active.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  active.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// The carryover destination must be a planned sprint.
	if _, _, err := service.Complete(ctx, CompleteInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  active.ID,

		CarryoverSprintID: &active.ID,
	}); !errors.Is(err, ErrCarryoverTargetInvalid) {
		t.Fatalf("self carryover error = %v", err)
	}

	_, report, err := service.Complete(ctx, CompleteInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  active.ID,

		CarryoverSprintID: &next.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.CarryoverTaskCount != 1 {
		t.Fatalf("carryover = %d, want 1", report.CarryoverTaskCount)
	}

	var sprintID *string
	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id::text FROM tasks WHERE id = $1`,
		task,
	).Scan(&sprintID); err != nil {
		t.Fatal(err)
	}

	if sprintID == nil || *sprintID != next.ID {
		t.Fatalf("task sprint = %v, want %s", sprintID, next.ID)
	}
}

func TestSprintOneActiveSprintPerProject(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")

	alice := sprintIdentity("alice")

	end := time.Now().Add(24 * time.Hour)

	first, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "First",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Second",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  first.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  second.ID,
	}); !errors.Is(err, ErrActiveSprintExists) {
		t.Fatalf("second start error = %v, want ErrActiveSprintExists", err)
	}
}

func TestSprintDeleteReturnsTasksToBacklog(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	statusID := insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")

	alice := sprintIdentity("alice")

	end := time.Now().Add(24 * time.Hour)

	sprint, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Planned",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"task", nil, false,
	)

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: alice,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.Delete(ctx, DeleteInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	var sprintID any
	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id FROM tasks WHERE id = $1`,
		task,
	).Scan(&sprintID); err != nil {
		t.Fatal(err)
	}
	if sprintID != nil {
		t.Fatal("deleted sprint left its task assigned")
	}

	// An active sprint cannot be deleted.
	active, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Active",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  active.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.Delete(ctx, DeleteInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  active.ID,
	}); !errors.Is(err, ErrLifecycleConflict) {
		t.Fatalf("delete active sprint error = %v", err)
	}
}

func TestSprintValidation(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")

	alice := sprintIdentity("alice")

	end := time.Now().Add(24 * time.Hour)
	start := time.Now().Add(48 * time.Hour)

	if _, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "   ",

		PlannedEndAt: &end,
	}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("empty name error = %v, want ErrInvalidName", err)
	}

	if _, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Sprint",

		PlannedStartAt: &start,
		PlannedEndAt:   &end,
	}); !errors.Is(err, ErrInvalidDateRange) {
		t.Fatalf("inverted range error = %v, want ErrInvalidDateRange", err)
	}

	if _, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Sprint",
	}); !errors.Is(err, ErrInvalidDateRange) {
		t.Fatalf("missing end error = %v, want ErrInvalidDateRange", err)
	}

	state := State("nonsense")

	if _, err := service.List(ctx, ListInput{
		Identity: alice,

		ProjectID: projectID,

		State: &state,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid state error = %v, want ErrInvalidState", err)
	}
}
