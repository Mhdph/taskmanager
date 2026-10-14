package task

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/project"
)

func TestTimeEntryLifecycleAndAccess(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	item, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", Name: "timers", CreatedByUserID: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	b := createTaskForTest(t, tasks, ctx, item.ID, statuses[0].ID)
	for _, member := range []struct {
		id   string
		role project.MemberRole
	}{{"member", project.MemberRoleMember}, {"viewer", project.MemberRoleViewer}} {
		if _, err := projects.AddMember(ctx, project.AddMemberParams{TenantID: "tenant-a", ProjectID: item.ID, UserID: member.id, Role: member.role}); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(tasks, projects, project.NewAuthorizer(projects), nil, nil, nil, 0)
	input := TimeInput{Identity: auth.Identity{TenantID: "tenant-a", UserID: "member", Role: auth.RoleMember}, ProjectID: item.ID, TaskID: a.ID}
	viewer := input
	viewer.Identity.UserID = "viewer"
	if _, err := service.StartTimer(ctx, viewer); !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("viewer start: %v", err)
	}
	entry, err := service.StartTimer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if entry.EndedAt != nil || entry.DurationSeconds != 0 {
		t.Fatal("new timer is not active")
	}
	second := input
	second.TaskID = b.ID
	if _, err := service.StartTimer(ctx, second); !errors.Is(err, ErrTimerActive) {
		t.Fatalf("second active timer: %v", err)
	}
	if _, err := service.StopTimer(ctx, viewer); !errors.Is(err, ErrTimerNotFound) {
		t.Fatalf("another user stopped timer: %v", err)
	}
	if err := service.DeleteTimeEntry(ctx, input, entry.ID); !errors.Is(err, ErrTimeEntryNotFound) {
		t.Fatalf("active timer deleted: %v", err)
	}
	if _, err := tasks.db.Exec(ctx, `UPDATE task_time_entries SET started_at = NOW() - INTERVAL '65 seconds' WHERE id = $1`, entry.ID); err != nil {
		t.Fatal(err)
	}
	stopped, err := service.StopTimer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.EndedAt == nil || stopped.DurationSeconds < 65 {
		t.Fatalf("invalid stopped timer: %+v", stopped)
	}
	p, err := service.ListTimeEntries(ctx, viewer, 20, "")
	if err != nil || len(p.Items) != 1 {
		t.Fatalf("viewer list: %+v, %v", p, err)
	}
	projectUUID := uuid.MustParse(item.ID)
	summary := db.GetProjectTrackedSecondsParams{TenantID: "tenant-a", ProjectID: pgtype.UUID{Bytes: projectUUID, Valid: true}}
	seconds, err := tasks.queries.GetProjectTrackedSeconds(ctx, summary)
	if err != nil || seconds != stopped.DurationSeconds {
		t.Fatalf("dashboard total: %d, %v", seconds, err)
	}
	other := input
	other.Identity.UserID = "manager"
	if err := service.DeleteTimeEntry(ctx, other, entry.ID); !errors.Is(err, ErrTimeEntryNotFound) {
		t.Fatalf("another user deleted entry: %v", err)
	}
	if err := service.DeleteTimeEntry(ctx, input, entry.ID); err != nil {
		t.Fatal(err)
	}
	seconds, err = tasks.queries.GetProjectTrackedSeconds(ctx, summary)
	if err != nil || seconds != 0 {
		t.Fatalf("deleted entry remained in dashboard: %d, %v", seconds, err)
	}
	if _, err := service.StartTimer(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := projects.DeleteMember(ctx, "tenant-a", item.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListTimeEntries(ctx, input, 20, ""); err == nil {
		t.Fatal("removed member can list entries")
	}
	if _, err := service.StopTimer(ctx, input); err != nil {
		t.Fatalf("removed owner cannot stop timer: %v", err)
	}
}

func TestConcurrentTimerStartsAllowOnlyOne(t *testing.T) {
	tasks, projects, ctx := integrationTaskRepository(t)
	item, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", Name: "concurrent timers", CreatedByUserID: "manager"})
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
	for i, id := range []string{a.ID, b.ID} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			<-start
			scope, err := parseTimeScope("tenant-a", item.ID, id, "manager")
			if err != nil {
				errs[i] = err
				return
			}
			_, errs[i] = tasks.startTimer(ctx, scope)
		}(i, id)
	}
	close(start)
	wg.Wait()
	successes, rejections := 0, 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrTimerActive) {
			rejections++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatalf("concurrent starts: %v", errs)
	}
}
