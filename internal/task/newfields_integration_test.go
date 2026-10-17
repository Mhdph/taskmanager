package task

import (
	"errors"
	"testing"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

// TestLabelDirectoryAndTaskAttachment covers the label CRUD plus the
// task-level attachment endpoint semantics (replace set, cascade delete).
func TestLabelDirectoryAndTaskAttachment(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fixture := setupWorkspace(t, taskRepo, projectRepo, ctx)

	service := workspaceService(taskRepo, projectRepo)

	admin := auth.Identity{
		UserID:   "admin-user",
		TenantID: workspaceTenant,
		Role:     auth.RoleAdmin,
	}

	member := auth.Identity{
		UserID:   "member-user",
		TenantID: workspaceTenant,
	}

	// Members cannot manage the label directory.
	if _, err := service.CreateLabel(ctx, CreateLabelInput{
		Identity: member,
		Name:     "design",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member create label err = %v, want ErrForbidden", err)
	}

	design, err := service.CreateLabel(ctx, CreateLabelInput{
		Identity: admin,
		Name:     "design",
		Color:    "#ff0000",
	})
	if err != nil {
		t.Fatal(err)
	}

	if design.Name != "design" || design.Color != "#ff0000" {
		t.Fatalf("unexpected label: %+v", design)
	}

	// Duplicate names within a tenant conflict.
	if _, err := service.CreateLabel(ctx, CreateLabelInput{
		Identity: admin,
		Name:     "design",
	}); !isLabelConflict(err) {
		t.Fatalf("duplicate label err = %v, want conflict", err)
	}

	urgent, err := service.CreateLabel(ctx, CreateLabelInput{
		Identity: admin,
		Name:     "urgent",
	})
	if err != nil {
		t.Fatal(err)
	}

	labels, err := service.ListLabels(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}

	if len(labels) != 2 {
		t.Fatalf("label count = %d, want 2", len(labels))
	}

	taskA := fixture.tasks["a-low-nodue"]

	// Attach both labels to a task of project A as its member.
	attached, err := service.SetTaskLabels(ctx, SetTaskLabelsInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		LabelIDs: []string{design.ID, urgent.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(attached) != 2 {
		t.Fatalf("attached count = %d, want 2", len(attached))
	}

	// Unknown labels are rejected.
	if _, err := service.SetTaskLabels(ctx, SetTaskLabelsInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		LabelIDs: []string{"not-a-uuid"},
	}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("bad label id err = %v, want ErrInvalidID", err)
	}

	if _, err := service.SetTaskLabels(ctx, SetTaskLabelsInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		LabelIDs: []string{"00000000-0000-0000-0000-000000000000"},
	}); !errors.Is(err, ErrLabelNotFound) {
		t.Fatalf("missing label err = %v, want ErrLabelNotFound", err)
	}

	// Detail reads carry the labels.
	detail, err := service.Get(ctx, member, fixture.projects["A"].ID, taskA.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(detail.Labels) != 2 {
		t.Fatalf("detail label count = %d, want 2", len(detail.Labels))
	}

	// Replace clears the previous set.
	attached, err = service.SetTaskLabels(ctx, SetTaskLabelsInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		LabelIDs: []string{urgent.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(attached) != 1 || attached[0].ID != urgent.ID {
		t.Fatalf("unexpected replacement set: %+v", attached)
	}

	// Deleting the label removes it from every task.
	if err := service.DeleteLabel(ctx, admin, urgent.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SetTaskLabels(ctx, SetTaskLabelsInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		LabelIDs: []string{urgent.ID},
	}); !errors.Is(err, ErrLabelNotFound) {
		t.Fatalf("deleted label err = %v, want ErrLabelNotFound", err)
	}

	// Renaming works and propagates on reads.
	renamed, err := service.UpdateLabel(ctx, UpdateLabelInput{
		Identity: admin,
		LabelID:  design.ID,
		Name:     strPtr("design-v2"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if renamed.Name != "design-v2" {
		t.Fatalf("renamed label name = %q", renamed.Name)
	}
}

func strPtr(value string) *string {
	return &value
}

// TestTaskFieldUpdates covers estimate and recurrence updates, including
// validation errors and clearing semantics.
func TestTaskFieldUpdates(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fixture := setupWorkspace(t, taskRepo, projectRepo, ctx)

	service := workspaceService(taskRepo, projectRepo)

	member := auth.Identity{
		UserID:   "member-user",
		TenantID: workspaceTenant,
	}

	taskA := fixture.tasks["a-low-nodue"]

	estimate := int64(7200)
	recurrence := "weekly"

	updated, err := service.Update(ctx, UpdateInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		UpdateEstimate:   true,
		EstimateSeconds:  &estimate,
		UpdateRecurrence: true,
		Recurrence:       &recurrence,
	})
	if err != nil {
		t.Fatal(err)
	}

	if updated.EstimateSeconds == nil || *updated.EstimateSeconds != estimate {
		t.Fatalf("estimate = %v, want %d", updated.EstimateSeconds, estimate)
	}

	if updated.Recurrence == nil || *updated.Recurrence != "weekly" {
		t.Fatalf("recurrence = %v, want weekly", updated.Recurrence)
	}

	// Whitelist violations are rejected.
	bad := "annually"
	if _, err := service.Update(ctx, UpdateInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		UpdateRecurrence: true,
		Recurrence:       &bad,
	}); !errors.Is(err, ErrInvalidRecurrence) {
		t.Fatalf("bad recurrence err = %v, want ErrInvalidRecurrence", err)
	}

	negative := int64(-1)
	if _, err := service.Update(ctx, UpdateInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		UpdateEstimate:  true,
		EstimateSeconds: &negative,
	}); !errors.Is(err, ErrInvalidEstimate) {
		t.Fatalf("bad estimate err = %v, want ErrInvalidEstimate", err)
	}

	tooLarge := int64(maxEstimateSeconds + 1)
	if _, err := service.Update(ctx, UpdateInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		UpdateEstimate:  true,
		EstimateSeconds: &tooLarge,
	}); !errors.Is(err, ErrInvalidEstimate) {
		t.Fatalf("oversized estimate err = %v, want ErrInvalidEstimate", err)
	}

	// Clearing both via explicit nulls works.
	cleared, err := service.Update(ctx, UpdateInput{
		Identity: member,

		ProjectID: fixture.projects["A"].ID,
		TaskID:    taskA.ID,

		UpdateEstimate:   true,
		EstimateSeconds:  nil,
		UpdateRecurrence: true,
		Recurrence:       nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	if cleared.EstimateSeconds != nil || cleared.Recurrence != nil {
		t.Fatalf("fields not cleared: %+v", cleared)
	}
}

// TestReorderAndPositionSort covers the reorder endpoint, the default
// position assignment on create, and keyset pagination over positions.
func TestReorderAndPositionSort(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fixture := setupWorkspace(t, taskRepo, projectRepo, ctx)

	service := workspaceService(taskRepo, projectRepo)

	member := auth.Identity{
		UserID:   "member-user",
		TenantID: workspaceTenant,
	}

	projectA := fixture.projects["A"]

	// Mapper identity without admin rights sees only project A tasks.
	input := auth.Identity{
		UserID:   "mapper",
		TenantID: workspaceTenant,
	}

	if err := service.Reorder(ctx, ReorderInput{
		Identity:  input,
		ProjectID: projectA.ID,

		Tasks: []ReorderTask{
			{TaskID: fixture.tasks["a-low-nodue"].ID, Position: 100},
			{TaskID: fixture.tasks["a-urgent-due-past"].ID, Position: 200},
		},
	}); !errors.Is(err, project.ErrForbidden) &&
		!errors.Is(err, project.ErrNotFound) {

		t.Fatalf("outsider reorder err = %v, want forbidden or not-found", err)
	}

	if err := service.Reorder(ctx, ReorderInput{
		Identity:  member,
		ProjectID: projectA.ID,

		Tasks: []ReorderTask{
			{TaskID: fixture.tasks["a-low-nodue"].ID, Position: 100},
			{TaskID: fixture.tasks["a-urgent-due-past"].ID, Position: 200},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Reordering a task from another project fails with not-found.
	if err := service.Reorder(ctx, ReorderInput{
		Identity:  member,
		ProjectID: projectA.ID,

		Tasks: []ReorderTask{
			{TaskID: fixture.tasks["b-high"].ID, Position: 300},
		},
	}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-project reorder err = %v, want ErrTaskNotFound", err)
	}

	repoList, err := taskRepo.ListForTenant(ctx, workspaceTenant, member.UserID, WorkspaceListOptions{
		ProjectID: &projectA.ID,
		Sort:      WorkspaceSortPosition,
		Desc:      false,
		Limit:     100,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(repoList) != 4 {
		t.Fatalf("project task count = %d, want 4", len(repoList))
	}

	// Sorted ascending: the reordered tasks first, then the tasks that
	// carry their creation default positions.
	if *repoList[0].Position != 100 || *repoList[1].Position != 200 {
		t.Fatalf("position order wrong: %v, %v", *repoList[0].Position, *repoList[1].Position)
	}

	for i := 1; i < len(repoList); i++ {
		if *repoList[i].Position < *repoList[i-1].Position {
			t.Fatalf("positions not sorted ascending: %v after %v", *repoList[i].Position, *repoList[i-1].Position)
		}
	}

	// Cursor pagination walks the whole position ordering without loss.
	positions := collectPaginated(t, taskRepo, ctx, auth.Identity{
		UserID:   "admin-user",
		TenantID: workspaceTenant,
	}, WorkspaceListOptions{
		IsAdmin: true,
		Sort:    WorkspaceSortPosition,
		Desc:    false,
	}, 2)

	// 5 tasks: 2 positioned + created tasks have default positions
	// (this includes project B tasks for the admin).
	if len(positions) != 5 {
		t.Fatalf("paginated count = %d, want 5", len(positions))
	}

	unique := map[string]struct{}{}

	for _, id := range positions {
		if _, seen := unique[id]; seen {
			t.Fatalf("duplicate task %s in position pages", id)
		}

		unique[id] = struct{}{}
	}

	// Descending direction flips pages correctly.
	descPositions := collectPaginated(t, taskRepo, ctx, auth.Identity{
		UserID:   "admin-user",
		TenantID: workspaceTenant,
	}, WorkspaceListOptions{
		IsAdmin: true,
		Sort:    WorkspaceSortPosition,
		Desc:    true,
	}, 2)

	if len(descPositions) != len(positions) {
		t.Fatalf("desc count = %d, want %d", len(descPositions), len(positions))
	}

	if descPositions[0] == positions[0] && descPositions[len(descPositions)-1] == positions[len(positions)-1] {
		t.Fatalf("desc order identical to asc")
	}
}

// TestDefaultPosition increments per status column: newly created tasks
// land after every existing task in the same column.
func TestDefaultPosition(t *testing.T) {
	taskRepo, projectRepo, ctx := integrationWorkspaceSetup(t)
	fixture := setupWorkspace(t, taskRepo, projectRepo, ctx)

	statuses, err := projectRepo.ListStatuses(ctx, workspaceTenant, fixture.projects["A"].ID)
	if err != nil {
		t.Fatal(err)
	}

	todo := statuses[0]

	// Seed always used todo for exactly one task ("a-urgent-due-past").
	first := fixture.tasks["a-urgent-due-past"]

	if first.Position == nil {
		t.Fatalf("seeded task missing default position")
	}

	second, err := taskRepo.Create(ctx, CreateParams{
		ID:              "00000000-0000-0000-0000-0000000000ab",
		TenantID:        workspaceTenant,
		ProjectID:       fixture.projects["A"].ID,
		StatusID:        todo.ID,
		Title:           "second todo",
		Priority:        PriorityMedium,
		CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}

	if second.Position == nil || *second.Position <= *first.Position {
		t.Fatalf("second position %v not after first %v", second.Position, first.Position)
	}
}
