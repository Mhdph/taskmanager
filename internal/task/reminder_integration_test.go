package task

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
)

type reminderSenderFunc func(context.Context, notification.SendInput, string) error

func (f reminderSenderFunc) SendWithID(ctx context.Context, input notification.SendInput, key string) error {
	return f(ctx, input, key)
}

func reminderTestInput(f deliveryFixture, user string) ReminderInput {
	return ReminderInput{Identity: f.user(user).Identity, ProjectID: f.input.ProjectID, TaskID: f.input.TaskID}
}

func forceReminderDue(t *testing.T, f deliveryFixture, id string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_reminders SET remind_at=NOW()-INTERVAL '1 second', minutes_before_due=NULL, next_attempt_at=NOW() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalReminderLifecycleAndIsolation(t *testing.T) {
	f := newDeliveryFixture(t)
	alice, bob := reminderTestInput(f, "alice"), reminderTestInput(f, "bob")
	future := time.Now().Add(time.Hour)
	first, err := f.service.CreateReminder(f.ctx, CreateReminderInput{ReminderInput: alice, RemindAt: &future, Note: "پیگیری"})
	if err != nil || first.Status != "scheduled" {
		t.Fatalf("create: %+v %v", first, err)
	}
	second, err := f.service.CreateReminder(f.ctx, CreateReminderInput{ReminderInput: alice, RemindAt: &future})
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.service.ListReminders(f.ctx, alice, 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("page: %+v %v", page, err)
	}
	next, err := f.service.ListReminders(f.ctx, alice, 1, *page.NextCursor)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.NextCursor != nil {
		t.Fatalf("next page: %+v %v", next, err)
	}
	page, err = f.service.ListReminders(f.ctx, bob, 20, "")
	if err != nil || len(page.Items) != 0 {
		t.Fatal("another member can see reminders")
	}
	if err := f.service.CancelReminder(f.ctx, bob, first.ID); !errors.Is(err, ErrReminderNotFound) {
		t.Fatalf("other user cancel: %v", err)
	}
	later := future.Add(time.Hour)
	if _, err := f.service.SnoozeReminder(f.ctx, bob, first.ID, later); !errors.Is(err, ErrReminderNotFound) {
		t.Fatalf("other user snooze: %v", err)
	}
	snoozed, err := f.service.SnoozeReminder(f.ctx, alice, first.ID, later)
	if err != nil || !snoozed.RemindAt.Equal(later) || snoozed.MinutesBeforeDue != nil {
		t.Fatalf("snooze: %+v %v", snoozed, err)
	}
	if err := f.service.CancelReminder(f.ctx, alice, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.CancelReminder(f.ctx, alice, first.ID); err != nil {
		t.Fatalf("cancel not idempotent: %v", err)
	}
	if _, err := f.service.SnoozeReminder(f.ctx, alice, first.ID, later); !errors.Is(err, ErrReminderCancelled) {
		t.Fatalf("cancelled snoozed: %v", err)
	}
	viewer := reminderTestInput(f, "viewer")
	if _, err := f.service.CreateReminder(f.ctx, CreateReminderInput{ReminderInput: viewer, RemindAt: &future}); err != nil {
		t.Fatalf("viewer personal reminder: %v", err)
	}
	otherTenant := alice
	otherTenant.Identity.TenantID = "tenant-b"
	if err := f.service.CancelReminder(f.ctx, otherTenant, second.ID); !errors.Is(err, ErrReminderNotFound) {
		t.Fatalf("cross-tenant cancel: %v", err)
	}
	if _, err := f.service.ListReminders(f.ctx, otherTenant, 20, ""); err == nil {
		t.Fatal("cross-tenant list allowed")
	}
	if err := f.projects.DeleteMember(f.ctx, "tenant-a", alice.ProjectID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.CancelReminder(f.ctx, alice, second.ID); err != nil {
		t.Fatalf("former member cannot cancel own reminder: %v", err)
	}
}

func TestReminderRetriesPersistAndSnoozeGetsNewKey(t *testing.T) {
	f := newDeliveryFixture(t)
	input := reminderTestInput(f, "alice")
	at := time.Now().Add(time.Hour)
	reminder, err := f.service.CreateReminder(f.ctx, CreateReminderInput{ReminderInput: input, RemindAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	forceReminderDue(t, f, reminder.ID)
	var keys []string
	fail := true
	sender := reminderSenderFunc(func(_ context.Context, payload notification.SendInput, key string) error {
		keys = append(keys, key)
		if payload.RecipientUserID != "alice" || payload.ActorUserID != nil {
			t.Fatal("wrong recipient/actor")
		}
		if fail {
			return errors.New("provider unavailable")
		}
		return nil
	})
	found, err := f.service.repository.deliverReminder(f.ctx, sender)
	if err != nil || !found {
		t.Fatalf("first attempt: %v %v", found, err)
	}
	var status string
	var attempts int
	var retry time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT status,attempt_count,next_attempt_at FROM task_reminders WHERE id=$1`, reminder.ID).Scan(&status, &attempts, &retry); err != nil {
		t.Fatal(err)
	}
	if status != "scheduled" || attempts != 1 || !retry.After(time.Now()) {
		t.Fatalf("failed reminder consumed: %s %d %s", status, attempts, retry)
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || found {
		t.Fatalf("backoff ignored: %v %v", found, err)
	}
	fail = false
	forceReminderDue(t, f, reminder.ID)
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || !found {
		t.Fatalf("retry: %v %v", found, err)
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatal("retry idempotency key changed")
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || found {
		t.Fatalf("sent twice: %v %v", found, err)
	}
	if _, err := f.service.SnoozeReminder(f.ctx, input, reminder.ID, at); err != nil {
		t.Fatal(err)
	}
	forceReminderDue(t, f, reminder.ID)
	if _, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys[2] == keys[1] {
		t.Fatal("snooze reused prior delivery key")
	}
}

func TestRelativeReminderFollowsDeadlineAndSkipsInactiveTasks(t *testing.T) {
	f := newDeliveryFixture(t)
	input := reminderTestInput(f, "alice")
	due := time.Now().Add(48 * time.Hour)
	if _, err := f.pool.Exec(f.ctx, `UPDATE tasks SET due_at=$2 WHERE id=$1`, input.TaskID, due); err != nil {
		t.Fatal(err)
	}
	minutes := int32(1440)
	r, err := f.service.CreateReminder(f.ctx, CreateReminderInput{ReminderInput: input, MinutesBeforeDue: &minutes})
	if err != nil {
		t.Fatal(err)
	}
	sender := reminderSenderFunc(func(context.Context, notification.SendInput, string) error { return nil })
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || found {
		t.Fatal("future reminder sent")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE tasks SET due_at=NULL WHERE id=$1`, input.TaskID); err != nil {
		t.Fatal(err)
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || found {
		t.Fatal("missing due date did not pause reminder")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE tasks SET due_at=NOW()+INTERVAL '1 hour', completed_at=NOW() WHERE id=$1`, input.TaskID); err != nil {
		t.Fatal(err)
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || found {
		t.Fatal("completed task reminded")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE tasks SET completed_at=NULL, archived_at=NOW() WHERE id=$1`, input.TaskID); err != nil {
		t.Fatal(err)
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || found {
		t.Fatal("archived task reminded")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE tasks SET archived_at=NULL WHERE id=$1`, input.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE projects SET archived_at=NOW() WHERE id=$1`, input.ProjectID); err != nil {
		t.Fatal(err)
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || found {
		t.Fatal("archived project reminded")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE projects SET archived_at=NULL WHERE id=$1`, input.ProjectID); err != nil {
		t.Fatal(err)
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, sender); err != nil || !found {
		t.Fatalf("overdue reminder after restore missed: %v %v", found, err)
	}
	page, err := f.service.ListReminders(f.ctx, input, 20, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != r.ID || page.Items[0].Status != "sent" {
		t.Fatalf("sent state: %+v %v", page, err)
	}
}

func TestReminderLimitAndConcurrentWorkers(t *testing.T) {
	f := newDeliveryFixture(t)
	input := reminderTestInput(f, "alice")
	at := time.Now().Add(time.Hour)
	for i := 0; i < MaxTaskReminders-1; i++ {
		if _, err := f.service.CreateReminder(f.ctx, CreateReminderInput{ReminderInput: input, RemindAt: &at}); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	errs := make([]error, 2)
	results := make([]Reminder, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = f.service.CreateReminder(f.ctx, CreateReminderInput{ReminderInput: input, RemindAt: &at})
		}(i)
	}
	close(start)
	wg.Wait()
	successes, limits := 0, 0
	var id string
	for i, err := range errs {
		if err == nil {
			successes++
			id = results[i].ID
		} else if errors.Is(err, ErrReminderLimit) {
			limits++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || limits != 1 {
		t.Fatalf("limit raced: %d successes %d limits", successes, limits)
	}
	forceReminderDue(t, f, id)
	entered, release := make(chan struct{}), make(chan struct{})
	sender := reminderSenderFunc(func(ctx context.Context, _ notification.SendInput, _ string) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() { _, err := f.service.repository.deliverReminder(f.ctx, sender); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not send")
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, reminderSenderFunc(func(context.Context, notification.SendInput, string) error {
		t.Error("second worker sent locked reminder")
		return nil
	})); err != nil || found {
		t.Errorf("second worker: %v %v", found, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRemovedReminderOwnerIsNotNotified(t *testing.T) {
	f := newDeliveryFixture(t)
	input := reminderTestInput(f, "alice")
	at := time.Now().Add(time.Hour)
	r, err := f.service.CreateReminder(f.ctx, CreateReminderInput{ReminderInput: input, RemindAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	forceReminderDue(t, f, r.ID)
	if err := f.projects.DeleteMember(f.ctx, "tenant-a", input.ProjectID, "alice"); err != nil {
		t.Fatal(err)
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, reminderSenderFunc(func(context.Context, notification.SendInput, string) error {
		t.Error("notified a former member")
		return nil
	})); err != nil || found {
		t.Fatalf("removed member: %v %v", found, err)
	}
	if _, err := f.projects.AddMember(f.ctx, project.AddMemberParams{TenantID: "tenant-a", ProjectID: input.ProjectID, UserID: "alice", Role: project.MemberRoleMember}); err != nil {
		t.Fatal(err)
	}
	if found, err := f.service.repository.deliverReminder(f.ctx, reminderSenderFunc(func(context.Context, notification.SendInput, string) error { return nil })); err != nil || !found {
		t.Fatalf("restored member: %v %v", found, err)
	}
}
