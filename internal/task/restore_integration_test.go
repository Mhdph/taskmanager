package task

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

func TestArchiveListingAndRestore(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	item, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", Name: "archive", CreatedByUserID: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	b := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	if _, err := projects.AddMember(ctx, project.AddMemberParams{TenantID: "tenant-a", ProjectID: item.ID, UserID: "member", Role: project.MemberRoleMember}); err != nil {
		t.Fatal(err)
	}
	authorizer := project.NewAuthorizer(projects)
	projectService := project.NewService(projects, authorizer)
	service := NewService(tasks, projects, authorizer, nil, nil, nil, 0)
	manager := auth.Identity{TenantID: "tenant-a", UserID: "manager", Role: auth.RoleMember}
	member := auth.Identity{TenantID: "tenant-a", UserID: "member", Role: auth.RoleMember}
	for _, id := range []string{a.ID, b.ID} {
		if err := service.Archive(ctx, manager, item.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	active, err := service.List(ctx, ListInput{Identity: manager, ProjectID: item.ID})
	if err != nil || len(active.Items) != 0 {
		t.Fatalf("active task list: %+v %v", active, err)
	}
	page, err := service.List(ctx, ListInput{Identity: member, ProjectID: item.ID, Archived: true, Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("archived task page: %+v %v", page, err)
	}
	second, err := service.List(ctx, ListInput{Identity: member, ProjectID: item.ID, Archived: true, Limit: 1, Cursor: *page.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == page.Items[0].ID || second.NextCursor != nil {
		t.Fatalf("second archived page: %+v %v", second, err)
	}
	if _, err := service.Restore(ctx, member, item.ID, a.ID); !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("member restore: %v", err)
	}
	foreign := auth.Identity{TenantID: "tenant-b", UserID: "manager", Role: auth.RoleAdmin}
	if _, err := projectService.Restore(ctx, foreign, item.ID); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("cross-tenant restore: %v", err)
	}
	if err := projectService.Archive(ctx, manager, item.ID); err != nil {
		t.Fatal(err)
	}
	archivedProjects, err := projectService.List(ctx, project.ListInput{Identity: member, Archived: true})
	if err != nil || len(archivedProjects.Items) != 1 || archivedProjects.Items[0].ArchivedAt == nil {
		t.Fatalf("archived projects: %+v %v", archivedProjects, err)
	}
	hidden, err := projectService.List(ctx, project.ListInput{Identity: foreign, Archived: true})
	if err != nil || len(hidden.Items) != 0 {
		t.Fatalf("cross-tenant archived list: %+v %v", hidden, err)
	}
	if _, err := service.Restore(ctx, manager, item.ID, a.ID); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("task restored before project: %v", err)
	}
	if _, err := projectService.Restore(ctx, member, item.ID); !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("member project restore: %v", err)
	}
	restored, err := projectService.Restore(ctx, manager, item.ID)
	if err != nil || restored.ArchivedAt != nil {
		t.Fatalf("project restore: %+v %v", restored, err)
	}
	archivedTasks, err := service.List(ctx, ListInput{Identity: manager, ProjectID: item.ID, Archived: true})
	if err != nil || len(archivedTasks.Items) != 2 {
		t.Fatalf("project restore changed task archives: %+v %v", archivedTasks, err)
	}
	recovered, err := service.Restore(ctx, manager, item.ID, a.ID)
	if err != nil || recovered.ArchivedAt != nil || recovered.Title != a.Title || recovered.StatusID != a.StatusID {
		t.Fatalf("task restore: %+v %v", recovered, err)
	}
	if _, err := service.Restore(ctx, manager, item.ID, a.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("already-active task restore: %v", err)
	}
	active, err = service.List(ctx, ListInput{Identity: member, ProjectID: item.ID})
	if err != nil || len(active.Items) != 1 || active.Items[0].ID != a.ID {
		t.Fatalf("restored active list: %+v %v", active, err)
	}
}
