package task

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/project"
)

func TestStatusCategoryCannotChangeWhileUsed(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	item, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID: uuid.NewString(), TenantID: "tenant-a", Name: "status category", CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	status := statuses[0]
	assigned := createTaskForTest(t, tasks, ctx, item.ID, status.ID)
	done := project.StatusCategoryDone
	name := "renamed"
	if _, err := projects.UpdateStatus(ctx, project.UpdateStatusParams{
		TenantID: "tenant-a", ProjectID: item.ID, StatusID: status.ID, Category: &done, Name: &name,
	}); !errors.Is(err, project.ErrStatusInUse) {
		t.Fatalf("used status category update error = %v, want ErrStatusInUse", err)
	}
	unchanged, err := projects.GetStatus(ctx, "tenant-a", item.ID, status.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Category != status.Category || unchanged.Name != status.Name {
		t.Fatal("rejected category change partially updated the status")
	}
	if _, err := projects.UpdateStatus(ctx, project.UpdateStatusParams{
		TenantID: "tenant-a", ProjectID: item.ID, StatusID: status.ID, Category: &status.Category, Name: &name,
	}); err != nil {
		t.Fatalf("renaming and keeping the same category failed: %v", err)
	}
	if err := tasks.Archive(ctx, "tenant-a", item.ID, assigned.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.UpdateStatus(ctx, project.UpdateStatusParams{
		TenantID: "tenant-a", ProjectID: item.ID, StatusID: status.ID, Category: &done,
	}); !errors.Is(err, project.ErrStatusInUse) {
		t.Fatalf("archived task must also prevent category changes, got %v", err)
	}

	unused, err := projects.CreateStatus(ctx, project.CreateStatusParams{
		ID: uuid.NewString(), TenantID: "tenant-a", ProjectID: item.ID,
		Name: "unused", Position: 3, Category: project.StatusCategoryTodo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.UpdateStatus(ctx, project.UpdateStatusParams{
		TenantID: "tenant-a", ProjectID: item.ID, StatusID: unused.ID, Category: &done,
	}); err != nil {
		t.Fatalf("empty status category change failed: %v", err)
	}
	// Simulate a caller that read the old category and supplied no completion time.
	created := createTaskForTest(t, tasks, ctx, item.ID, unused.ID)
	if created.CompletedAt == nil {
		t.Fatal("task creation used a stale category instead of the locked done category")
	}
	// Status updates also derive completion from the category inside the transaction.
	updated, err := tasks.Update(ctx, UpdateParams{
		TenantID: "tenant-a", ProjectID: item.ID, TaskID: created.ID, StatusID: &status.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CompletedAt != nil {
		t.Fatal("moving from done to todo did not clear completion time")
	}
}

func TestConcurrentCategoryChangeAndTaskCreationStayConsistent(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	item, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID: uuid.NewString(), TenantID: "tenant-a", Name: "category race", CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	status := statuses[0]
	done := project.StatusCategoryDone
	start := make(chan struct{})
	var created Task
	var createErr, categoryErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		created, createErr = tasks.Create(ctx, CreateParams{
			ID: uuid.NewString(), TenantID: "tenant-a", ProjectID: item.ID, StatusID: status.ID,
			Title: "concurrent task", Priority: PriorityMedium, CreatedByUserID: "manager",
		})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, categoryErr = projects.UpdateStatus(ctx, project.UpdateStatusParams{
			TenantID: "tenant-a", ProjectID: item.ID, StatusID: status.ID, Category: &done,
		})
	}()
	close(start)
	wg.Wait()
	if createErr != nil {
		t.Fatal(createErr)
	}
	if categoryErr != nil && !errors.Is(categoryErr, project.ErrStatusInUse) {
		t.Fatal(categoryErr)
	}
	current, err := projects.GetStatus(ctx, "tenant-a", item.ID, status.ID)
	if err != nil {
		t.Fatal(err)
	}
	if (current.Category == done) != (created.CompletedAt != nil) {
		t.Fatalf("category %s disagrees with completed_at %v", current.Category, created.CompletedAt)
	}
}
