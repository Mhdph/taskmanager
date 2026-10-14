package task

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/project"
)

func TestTaskParentValidation(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	item, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID: uuid.NewString(), TenantID: "tenant-a", Name: "hierarchy", CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	b := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	c := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	update := func(taskID string, parent *string) (Task, error) {
		return tasks.Update(ctx, UpdateParams{
			TenantID: "tenant-a", ProjectID: item.ID, TaskID: taskID,
			UpdateParentTask: true, ParentTaskID: parent,
		})
	}
	if _, err := update(b.ID, &a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := update(c.ID, &b.ID); err != nil {
		t.Fatal(err)
	}

	otherProject, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID: uuid.NewString(), TenantID: "tenant-a", Name: "other", CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherStatuses, err := projects.ListStatuses(ctx, "tenant-a", otherProject.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreign := createTaskForTest(t, tasks, ctx, otherProject.ID, otherStatuses[0].ID)
	archived := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	if err := tasks.Archive(ctx, "tenant-a", item.ID, archived.ID); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		parent string
		want   error
	}{
		{"self", a.ID, ErrInvalidParentTask},
		{"direct descendant", b.ID, ErrInvalidParentTask},
		{"transitive descendant", c.ID, ErrInvalidParentTask},
		{"missing parent", uuid.NewString(), ErrParentTaskNotFound},
		{"other project", foreign.ID, ErrParentTaskNotFound},
		{"archived parent", archived.ID, ErrParentTaskNotFound},
		{"malformed parent id", "invalid", ErrInvalidID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := update(a.ID, &tt.parent); !errors.Is(err, tt.want) {
				t.Fatalf("parent update error = %v, want %v", err, tt.want)
			}
			current, err := tasks.GetForProject(ctx, "tenant-a", item.ID, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.ParentTaskID != nil {
				t.Fatal("rejected parent update changed the task")
			}
		})
	}
	updated, err := update(b.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ParentTaskID != nil {
		t.Fatal("clearing the parent did not detach the task")
	}
}

func TestConcurrentInverseParentsCannotBothCommit(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	item, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID: uuid.NewString(), TenantID: "tenant-a", Name: "concurrent parents", CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	b := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, pair := range [][2]string{{a.ID, b.ID}, {b.ID, a.ID}} {
		wg.Add(1)
		go func(i int, pair [2]string) {
			defer wg.Done()
			<-start
			_, errs[i] = tasks.Update(ctx, UpdateParams{
				TenantID: "tenant-a", ProjectID: item.ID, TaskID: pair[0],
				UpdateParentTask: true, ParentTaskID: &pair[1],
			})
		}(i, pair)
	}
	close(start)
	wg.Wait()
	successes, rejections := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrInvalidParentTask):
			rejections++
		default:
			t.Fatalf("unexpected parent update error: %v", err)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatalf("expected one success and one cycle rejection, got %v", errs)
	}
}
