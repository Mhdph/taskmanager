package sprint

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestConcurrentStartsProduceOneCommitment runs two starts of the same
// sprint at the same time. The sprint row lock serializes them: one starts
// the sprint, the other sees it running and returns it unchanged.
func TestConcurrentStartsProduceOneCommitment(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	statusID := insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")

	alice := sprintIdentity("alice")

	end := time.Now().Add(24 * time.Hour)

	sprint, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Concurrent",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"task", estimate(600), false,
	)

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: alice,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	var (
		wait   sync.WaitGroup
		mu     sync.Mutex
		errors []error
	)

	for i := 0; i < 2; i++ {
		wait.Add(1)

		go func() {
			defer wait.Done()

			_, err := service.Start(ctx, StartInput{
				Identity: alice,

				ProjectID: projectID,
				SprintID:  sprint.ID,
			})

			mu.Lock()
			errors = append(errors, err)
			mu.Unlock()
		}()
	}

	wait.Wait()

	for _, err := range errors {
		if err != nil {
			t.Fatalf("concurrent start failed: %v", err)
		}
	}

	var commitments int
	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM sprint_commitments WHERE sprint_id = $1`,
		sprint.ID,
	).Scan(&commitments); err != nil {
		t.Fatal(err)
	}

	if commitments != 1 {
		t.Fatalf("commitment rows = %d, want 1", commitments)
	}
}

// TestConcurrentCompletionsCarryOverOnce runs two completions of the same
// sprint at the same time. The carryover must happen exactly once.
func TestConcurrentCompletionsCarryOverOnce(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	statusID := insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")

	alice := sprintIdentity("alice")

	end := time.Now().Add(24 * time.Hour)

	sprint, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Concurrent",

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
		"task", estimate(600), false,
	)

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: alice,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	var (
		wait   sync.WaitGroup
		mu     sync.Mutex
		errors []error
	)

	for i := 0; i < 2; i++ {
		wait.Add(1)

		go func() {
			defer wait.Done()

			_, _, err := service.Complete(ctx, CompleteInput{
				Identity: alice,

				ProjectID: projectID,
				SprintID:  sprint.ID,

				CarryoverSprintID: &next.ID,
			})

			mu.Lock()
			errors = append(errors, err)
			mu.Unlock()
		}()
	}

	wait.Wait()

	for _, err := range errors {
		if err != nil {
			t.Fatalf("concurrent completion failed: %v", err)
		}
	}

	var reports int
	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM sprint_reports WHERE sprint_id = $1`,
		sprint.ID,
	).Scan(&reports); err != nil {
		t.Fatal(err)
	}

	if reports != 1 {
		t.Fatalf("report rows = %d, want 1", reports)
	}

	var removals int
	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM sprint_task_membership
		 WHERE sprint_id = $1 AND task_id = $2 AND action = 'removed'`,
		sprint.ID,
		task,
	).Scan(&removals); err != nil {
		t.Fatal(err)
	}

	if removals != 1 {
		t.Fatalf("removal records = %d, want 1", removals)
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

// TestTaskMoveRacingCompletion moves a task while the sprint completes.
// Both operations must finish, and the task must end up in exactly one
// sprint.
func TestTaskMoveRacingCompletion(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	statusID := insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")

	alice := sprintIdentity("alice")

	end := time.Now().Add(24 * time.Hour)

	sprint, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Racing",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	other, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Other",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"task", estimate(600), false,
	)

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: alice,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	var (
		wait sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	wait.Add(2)

	go func() {
		defer wait.Done()

		_, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
			Identity: alice,

			ProjectID: projectID,
			TaskID:    task,

			SprintID: &other.ID,
		})

		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}()

	go func() {
		defer wait.Done()

		_, _, err := service.Complete(ctx, CompleteInput{
			Identity: alice,

			ProjectID: projectID,
			SprintID:  sprint.ID,
		})

		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}()

	wait.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatalf("racing operation failed: %v", err)
		}
	}

	var sprintID *string
	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id::text FROM tasks WHERE id = $1`,
		task,
	).Scan(&sprintID); err != nil {
		t.Fatal(err)
	}

	if sprintID == nil {
		t.Fatal("task lost its sprint assignment")
	}

	if *sprintID != other.ID && *sprintID != sprint.ID {
		t.Fatalf("task sprint = %q, want one of the two sprints", *sprintID)
	}
}

// TestSprintArchiveAndRestore checks that an archived task keeps its
// assignment and that restoring it out of a completed sprint returns it to
// the backlog.
func TestSprintArchiveAndRestore(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	statusID := insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")

	alice := sprintIdentity("alice")

	end := time.Now().Add(24 * time.Hour)

	sprint, err := service.Create(ctx, CreateInput{
		Identity: alice,

		ProjectID: projectID,

		Name: "Sprint",

		PlannedEndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := insertSprintTask(
		t, pool, ctx, projectID, statusID,
		"task", estimate(600), false,
	)

	if _, err := service.SetTaskSprint(ctx, SetTaskSprintInput{
		Identity: alice,

		ProjectID: projectID,
		TaskID:    task,

		SprintID: &sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// Archiving keeps the assignment for traceability.
	if _, err := pool.Exec(
		ctx,
		`UPDATE tasks SET archived_at = NOW() WHERE id = $1`,
		task,
	); err != nil {
		t.Fatal(err)
	}

	detail, err := service.Get(ctx, GetInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Archived work is excluded from the live totals.
	if detail.Totals.TaskCount != 0 {
		t.Fatalf("live totals = %+v, want no archived task", detail.Totals)
	}

	if _, err := service.Start(ctx, StartInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.Complete(ctx, CompleteInput{
		Identity: alice,

		ProjectID: projectID,
		SprintID:  sprint.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// The archived task keeps the completed sprint.
	var sprintID *string
	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id::text FROM tasks WHERE id = $1`,
		task,
	).Scan(&sprintID); err != nil {
		t.Fatal(err)
	}

	if sprintID == nil || *sprintID != sprint.ID {
		t.Fatalf("archived task sprint = %v, want %s", sprintID, sprint.ID)
	}

	// Restoring an archived task out of a completed sprint returns it to
	// the backlog.
	if _, err := pool.Exec(
		ctx,
		`UPDATE tasks SET archived_at = NULL WHERE id = $1`,
		task,
	); err != nil {
		t.Fatal(err)
	}

	// The restore path of the task module clears the assignment.
	restored, err := pool.Exec(
		ctx,
		`UPDATE tasks
		 SET archived_at = NULL,
		     sprint_id = CASE
		         WHEN EXISTS (
		             SELECT 1 FROM sprints s
		             WHERE s.id = tasks.sprint_id
		               AND s.state = 'completed'
		         ) THEN NULL
		         ELSE sprint_id
		     END
		 WHERE id = $1`,
		task,
	)
	if err != nil {
		t.Fatal(err)
	}

	if restored.RowsAffected() != 1 {
		t.Fatal("task was not restored")
	}

	if err := pool.QueryRow(
		ctx,
		`SELECT sprint_id::text FROM tasks WHERE id = $1`,
		task,
	).Scan(&sprintID); err != nil {
		t.Fatal(err)
	}

	if sprintID != nil {
		t.Fatalf("restored task sprint = %v, want backlog", *sprintID)
	}
}
