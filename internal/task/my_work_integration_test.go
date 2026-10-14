package task

import (
	"testing"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/project"
)

func TestMyWorkRequiresCurrentProjectAccess(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	const tenantID = "tenant-a"
	const userID = "assignee"
	item, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID: uuid.NewString(), TenantID: tenantID, Name: "my work", CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.AddMember(ctx, project.AddMemberParams{
		TenantID: tenantID, ProjectID: item.ID, UserID: userID, Role: project.MemberRoleMember,
	}); err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, tenantID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	assignee := userID
	assigned, err := tasks.Create(ctx, CreateParams{
		ID: uuid.NewString(), TenantID: tenantID, ProjectID: item.ID,
		StatusID: statuses[0].ID, Title: "assigned task", Priority: PriorityMedium,
		CreatedByUserID: "manager", AssigneeUserID: &assignee,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertVisible := func(t *testing.T, isAdmin bool, wantVisible bool) {
		t.Helper()
		items, err := tasks.ListMyWork(ctx, tenantID, userID, MyWorkOptions{Limit: 20, IsAdmin: isAdmin})
		if err != nil {
			t.Fatal(err)
		}
		if wantVisible {
			if len(items) != 1 || items[0].ID != assigned.ID {
				t.Fatalf("my work = %v, want only task %s", items, assigned.ID)
			}
		} else if len(items) != 0 {
			t.Fatalf("my work returned %d tasks without project access", len(items))
		}
	}

	t.Run("current member sees assigned task", func(t *testing.T) {
		assertVisible(t, false, true)
	})
	if err := projects.DeleteMember(ctx, tenantID, item.ID, userID); err != nil {
		t.Fatal(err)
	}
	t.Run("removed member cannot see assigned task", func(t *testing.T) {
		assertVisible(t, false, false)
	})
	t.Run("admin retains access without membership", func(t *testing.T) {
		assertVisible(t, true, true)
	})
	if _, err := projects.AddMember(ctx, project.AddMemberParams{
		TenantID: tenantID, ProjectID: item.ID, UserID: userID, Role: project.MemberRoleMember,
	}); err != nil {
		t.Fatal(err)
	}
	if err := projects.Archive(ctx, tenantID, item.ID); err != nil {
		t.Fatal(err)
	}
	t.Run("archived project is hidden from members", func(t *testing.T) {
		assertVisible(t, false, false)
	})
	t.Run("archived project is hidden from admins", func(t *testing.T) {
		assertVisible(t, true, false)
	})
}
