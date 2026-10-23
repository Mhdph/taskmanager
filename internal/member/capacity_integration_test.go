package member

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

func TestMemberCapacityLifecycleAndAuthorization(t *testing.T) {
	repo, ctx := integrationMemberRepository(t)
	for _, user := range []string{"manager", "alice", "bob"} {
		if err := repo.Sync(ctx, SyncParams{TenantID: "a", UserID: user}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.UpdateRole(ctx, "a", "manager", RoleOwner); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo)
	manager := auth.Identity{TenantID: "a", UserID: "manager", Role: auth.RoleMember}
	alice := auth.Identity{TenantID: "a", UserID: "alice", Role: auth.RoleMember}
	unset, err := service.GetCapacity(ctx, alice, "alice")
	if err != nil || unset.Configured || unset.DailyCapacitySeconds != nil {
		t.Fatalf("default: %+v %v", unset, err)
	}
	input := CapacityInput{Identity: alice, UserID: "alice", DailyCapacitySeconds: 28800, WorkingDays: []int16{6, 7, 1, 2, 3}, Timezone: "Asia/Tehran"}
	if _, err := service.SetCapacity(ctx, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member changed capacity: %v", err)
	}
	input.Identity = manager
	saved, err := service.SetCapacity(ctx, input)
	if err != nil || !saved.Configured || *saved.DailyCapacitySeconds != 28800 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	if _, err := service.GetCapacity(ctx, alice, "bob"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other capacity visible: %v", err)
	}
	input.Identity = auth.Identity{TenantID: "other", UserID: "admin", Role: auth.RoleAdmin}
	if _, err := service.SetCapacity(ctx, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant capacity: %v", err)
	}
	if err := service.ClearCapacity(ctx, manager, "alice"); err != nil {
		t.Fatal(err)
	}
	unset, err = service.GetCapacity(ctx, alice, "alice")
	if err != nil || unset.Configured {
		t.Fatal("reset did not restore unknown capacity")
	}
	input.Identity = manager
	if _, err := service.SetCapacity(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, "a", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Sync(ctx, SyncParams{TenantID: "a", UserID: "alice"}); err != nil {
		t.Fatal(err)
	}
	unset, err = service.GetCapacity(ctx, alice, "alice")
	if err != nil || unset.Configured {
		t.Fatal("deleted member's capacity survived rejoining")
	}
}

func TestWorkloadEstimatesTrackingPrivacyAndPaging(t *testing.T) {
	repo, ctx := integrationMemberRepository(t)
	for _, user := range []string{"manager", "alice", "bob"} {
		if err := repo.Sync(ctx, SyncParams{TenantID: "a", UserID: user}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.UpdateRole(ctx, "a", "manager", RoleOwner); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo)
	manager := auth.Identity{TenantID: "a", UserID: "manager", Role: auth.RoleMember}
	alice := auth.Identity{TenantID: "a", UserID: "alice", Role: auth.RoleMember}
	if _, err := service.SetCapacity(ctx, CapacityInput{Identity: manager, UserID: "alice", DailyCapacitySeconds: 8 * 3600, WorkingDays: []int16{1, 2, 3, 4, 5}, Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	projects := project.NewRepository(repo.db, repo.queries)
	tasks := task.NewRepository(repo.db, repo.queries)
	makeProject := func(name string) (project.Project, string) {
		p, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: "a", Name: name, CreatedByUserID: "manager"})
		if err != nil {
			t.Fatal(err)
		}
		statuses, err := projects.ListStatuses(ctx, "a", p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return p, statuses[0].ID
	}
	shared, status := makeProject("shared")
	if _, err := projects.AddMember(ctx, project.AddMemberParams{TenantID: "a", ProjectID: shared.ID, UserID: "alice", Role: project.MemberRoleMember}); err != nil {
		t.Fatal(err)
	}
	assignee := "alice"
	create := func(pid, status string, start, due *time.Time, estimate *int64) task.Task {
		item, err := tasks.Create(ctx, task.CreateParams{ID: uuid.NewString(), TenantID: "a", ProjectID: pid, StatusID: status, Title: "workload task", Priority: task.PriorityMedium, AssigneeUserID: &assignee, CreatedByUserID: "manager", StartAt: start, DueAt: due, EstimateSeconds: estimate})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	start, end := workloadDate("2026-10-19"), workloadDate("2026-10-20")
	estimate := int64(16 * 3600)
	planned := create(shared.ID, status, &start, &end, &estimate)
	if _, err := repo.db.Exec(ctx, `INSERT INTO task_time_entries(id,tenant_id,project_id,task_id,user_id,started_at,ended_at,duration_seconds) VALUES($1,'a',$2,$3,'alice',NOW()-INTERVAL '2 hours',NOW(),7200)`, uuid.NewString(), shared.ID, planned.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(ctx, `INSERT INTO task_time_entries(id,tenant_id,project_id,task_id,user_id,started_at,ended_at,duration_seconds,deleted_at) VALUES($1,'a',$2,$3,'alice',NOW()-INTERVAL '10 hours',NOW(),36000,NOW())`, uuid.NewString(), shared.ID, planned.ID); err != nil {
		t.Fatal(err)
	}
	create(shared.ID, status, nil, nil, nil)
	backlog := int64(4 * 3600)
	create(shared.ID, status, nil, nil, &backlog)
	overdue := start.AddDate(0, 0, -1)
	late := int64(3 * 3600)
	create(shared.ID, status, nil, &overdue, &late)
	future := workloadDate("2026-12-01")
	create(shared.ID, status, nil, &future, &estimate)
	archived := create(shared.ID, status, nil, &end, &estimate)
	if _, err := repo.db.Exec(ctx, `UPDATE tasks SET archived_at=NOW() WHERE id=$1`, archived.ID); err != nil {
		t.Fatal(err)
	}
	completed := create(shared.ID, status, nil, &end, &estimate)
	if _, err := repo.db.Exec(ctx, `UPDATE tasks SET completed_at=NOW() WHERE id=$1`, completed.ID); err != nil {
		t.Fatal(err)
	}
	private, privateStatus := makeProject("private")
	privateEstimate := int64(4 * 3600)
	create(private.ID, privateStatus, nil, &end, &privateEstimate)
	input := WorkloadInput{Identity: alice, UserID: "alice", From: "2026-10-19", To: "2026-10-23", Limit: 50, Timezone: "UTC"}
	self, err := service.Workload(ctx, input)
	if err != nil || len(self.Items) != 1 {
		t.Fatalf("self report: %+v %v", self, err)
	}
	m := self.Items[0]
	if self.Scope != "visible_projects" || m.Totals.PlannedSeconds != 14*3600 || *m.Totals.CapacitySeconds != 40*3600 || m.UnscheduledSeconds != 4*3600 || m.UnestimatedTaskCount != 1 || m.OverdueSeconds != 3*3600 {
		t.Fatalf("incorrect workload or private leak: %+v", m)
	}
	input.Identity = manager
	full, err := service.Workload(ctx, input)
	if err != nil || full.Scope != "workspace" || full.Items[0].Totals.PlannedSeconds != 18*3600 {
		t.Fatalf("manager report: %+v %v", full, err)
	}
	input.Identity = alice
	input.UserID = "bob"
	if _, err := service.Workload(ctx, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("another user's report allowed: %v", err)
	}
	input.UserID = ""
	if _, err := service.Workload(ctx, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("full report allowed: %v", err)
	}
	input.Identity = manager
	input.Limit = 1
	first, err := service.Workload(ctx, input)
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("page: %+v %v", first, err)
	}
	input.Cursor = *first.NextCursor
	second, err := service.Workload(ctx, input)
	if err != nil || len(second.Items) != 1 || second.Items[0].UserID == first.Items[0].UserID {
		t.Fatalf("second page: %+v %v", second, err)
	}
	input.Identity = auth.Identity{TenantID: "other", UserID: "admin", Role: auth.RoleAdmin}
	input.Cursor = ""
	other, err := service.Workload(ctx, input)
	if err != nil || len(other.Items) != 0 {
		t.Fatal("cross-tenant report leaked data")
	}
}
