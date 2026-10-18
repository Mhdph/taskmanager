package task

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

func TestClaimTaskConcurrencyAndAccess(t *testing.T) {
	repo, projects, ctx := integrationTaskRepository(t)
	p, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{
		ID: uuid.NewString(), TenantID: "tenant-a", Name: "claims", CreatedByUserID: "manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		id   string
		role project.MemberRole
	}{
		{"alice", project.MemberRoleMember}, {"bob", project.MemberRoleMember}, {"viewer", project.MemberRoleViewer},
	} {
		if _, err := projects.AddMember(ctx, project.AddMemberParams{TenantID: "tenant-a", ProjectID: p.ID, UserID: m.id, Role: m.role}); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(repo, projects, project.NewAuthorizer(projects), nopNotificationSender{}, nil, nil, 0)
	item := createTaskForTest(t, repo, ctx, p.ID, statuses[0].ID)
	claim := func(ctx context.Context, user, tenant, task string, status *string) (Task, error) {
		return svc.Update(ctx, UpdateInput{
			Identity:  auth.Identity{TenantID: tenant, UserID: user, Role: auth.RoleMember},
			ProjectID: p.ID, TaskID: task, ClaimOnly: true, StatusID: status,
		})
	}
	if _, err := claim(ctx, "viewer", "tenant-a", item.ID, nil); !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("viewer claim: %v", err)
	}
	if _, err := claim(ctx, "outsider", "tenant-a", item.ID, nil); err == nil {
		t.Fatal("outsider claimed task")
	}
	if _, err := claim(ctx, "alice", "other-tenant", item.ID, nil); err == nil {
		t.Fatal("cross-tenant claim succeeded")
	}
	start := make(chan struct{})
	errs := make([]error, 2)
	users := []string{"alice", "bob"}
	var wg sync.WaitGroup
	for i := range users {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = claim(ctx, users[i], "tenant-a", item.ID, &statuses[i+1].ID)
		}(i)
	}
	close(start)
	wg.Wait()
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner != -1 {
				t.Fatal("two claims succeeded")
			}
			winner = i
		} else if !errors.Is(err, ErrTaskAlreadyAssigned) {
			t.Fatalf("unexpected claim error: %v", err)
		}
	}
	if winner == -1 {
		t.Fatalf("no claim succeeded: %v", errs)
	}
	current, err := repo.GetForProject(ctx, "tenant-a", p.ID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.AssigneeUserID == nil || *current.AssigneeUserID != users[winner] || current.StatusID != statuses[winner+1].ID {
		t.Fatalf("losing claim changed assignee or status: %+v", current)
	}
	if _, err := claim(ctx, users[winner], "tenant-a", item.ID, nil); err != nil {
		t.Fatalf("same-user retry: %v", err)
	}
	unassigned := createTaskForTest(t, repo, ctx, p.ID, statuses[0].ID)
	badStatus := uuid.NewString()
	if _, err := claim(ctx, "alice", "tenant-a", unassigned.ID, &badStatus); !errors.Is(err, ErrStatusNotFound) {
		t.Fatalf("invalid status: %v", err)
	}
	current, err = repo.GetForProject(ctx, "tenant-a", p.ID, unassigned.ID)
	if err != nil || current.AssigneeUserID != nil {
		t.Fatal("failed claim changed assignment")
	}
	if err := repo.Archive(ctx, "tenant-a", p.ID, unassigned.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := claim(ctx, "alice", "tenant-a", unassigned.ID, nil); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("archived claim: %v", err)
	}
}
