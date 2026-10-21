package task

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
)

type deliveryNotifications struct {
	mu    sync.Mutex
	items []notification.SendInput
}

func (n *deliveryNotifications) Send(_ context.Context, input notification.SendInput) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.items = append(n.items, input)
	return nil
}

type deliveryFixture struct {
	ctx           context.Context
	service       *Service
	projects      *project.Repository
	pool          *pgxpool.Pool
	input         DeliveryInput
	task          Task
	notifications *deliveryNotifications
}

func newDeliveryFixture(t *testing.T) deliveryFixture {
	t.Helper()
	tasks, projects, pool, ctx := integrationTaskRepositoryWithPool(t)
	p, err := projects.CreateWithDefaultStatuses(ctx, project.CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", Name: "deliveries", CreatedByUserID: "manager"})
	if err != nil {
		t.Fatal(err)
	}
	for user, role := range map[string]project.MemberRole{"alice": project.MemberRoleMember, "bob": project.MemberRoleMember, "charlie": project.MemberRoleMember, "viewer": project.MemberRoleViewer} {
		if _, err := projects.AddMember(ctx, project.AddMemberParams{TenantID: "tenant-a", ProjectID: p.ID, UserID: user, Role: role}); err != nil {
			t.Fatal(err)
		}
	}
	statuses, err := projects.ListStatuses(ctx, "tenant-a", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	assignee := "alice"
	item, err := tasks.Create(ctx, CreateParams{ID: uuid.NewString(), TenantID: "tenant-a", ProjectID: p.ID, StatusID: statuses[0].ID, Title: "تحویل خروجی", Priority: PriorityMedium, CreatedByUserID: "manager", AssigneeUserID: &assignee})
	if err != nil {
		t.Fatal(err)
	}
	notifications := &deliveryNotifications{}
	service := NewService(tasks, projects, project.NewAuthorizer(projects), notifications, nil, nil, 0)
	return deliveryFixture{ctx: ctx, service: service, projects: projects, pool: pool, task: item, notifications: notifications,
		input: DeliveryInput{Identity: auth.Identity{TenantID: "tenant-a", UserID: "alice", Role: auth.RoleMember}, ProjectID: p.ID, TaskID: item.ID}}
}

func (f deliveryFixture) user(id string) DeliveryInput {
	input := f.input
	input.Identity.UserID = id
	return input
}

func TestTaskDeliveryLifecycleAndHistory(t *testing.T) {
	f := newDeliveryFixture(t)
	first, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: "bob", Body: "نسخه اول"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != DeliveryPending || first.Revision != 1 || first.ReviewedAt != nil {
		t.Fatalf("invalid submission: %+v", first)
	}
	if _, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: "bob", Body: "duplicate"}); !errors.Is(err, ErrDeliveryPending) {
		t.Fatalf("duplicate pending: %v", err)
	}
	if _, err := f.service.ReviewDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.input, DeliveryID: first.ID, Decision: DeliveryApproved}); !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("self approval: %v", err)
	}
	if _, err := f.service.ReviewDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.user("manager"), DeliveryID: first.ID, Decision: DeliveryApproved}); !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("non-designated reviewer: %v", err)
	}
	review := ReviewDeliveryInput{DeliveryInput: f.user("bob"), DeliveryID: first.ID, Decision: DeliveryChangesRequested, Note: "معیار دوم کامل نیست"}
	returned, err := f.service.ReviewDelivery(f.ctx, review)
	if err != nil || returned.Status != DeliveryChangesRequested || returned.ReviewNote == nil || *returned.ReviewNote != review.Note || returned.ReviewedAt == nil || *returned.ReviewedByUserID != "bob" {
		t.Fatalf("return: %+v %v", returned, err)
	}
	if _, err := f.service.ReviewDelivery(f.ctx, review); !errors.Is(err, ErrDeliveryResolved) {
		t.Fatalf("repeated decision: %v", err)
	}
	output := "https://example.com/final"
	second, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: "bob", Body: "نسخه اصلاح‌شده", OutputURL: &output})
	if err != nil || second.Revision != 2 {
		t.Fatalf("resubmit: %+v %v", second, err)
	}
	approved, err := f.service.ReviewDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.user("bob"), DeliveryID: second.ID, Decision: DeliveryApproved})
	if err != nil || approved.Status != DeliveryApproved {
		t.Fatalf("approve: %+v %v", approved, err)
	}
	page, err := f.service.ListDeliveries(f.ctx, f.user("viewer"), 1, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != second.ID || page.NextCursor == nil {
		t.Fatalf("history: %+v %v", page, err)
	}
	older, err := f.service.ListDeliveries(f.ctx, f.user("viewer"), 1, *page.NextCursor)
	if err != nil || len(older.Items) != 1 || older.Items[0].ID != first.ID || older.Items[0].Body != "نسخه اول" || older.NextCursor != nil {
		t.Fatalf("old delivery changed: %+v %v", older, err)
	}
	stored, err := f.service.GetDelivery(f.ctx, f.input, second.ID)
	if err != nil || stored.OutputURL == nil || *stored.OutputURL != output {
		t.Fatalf("get: %+v %v", stored, err)
	}
	current, err := f.service.repository.GetForProject(f.ctx, "tenant-a", f.input.ProjectID, f.task.ID)
	if err != nil || current.StatusID != f.task.StatusID || current.CompletedAt != nil {
		t.Fatal("review unexpectedly changed board status")
	}
	var auditCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_logs WHERE task_id = $1 AND action LIKE 'task.delivery.%'`, f.task.ID).Scan(&auditCount); err != nil || auditCount != 4 {
		t.Fatalf("audit count %d: %v", auditCount, err)
	}
	if len(f.notifications.items) != 4 || f.notifications.items[0].RecipientUserID != "bob" || f.notifications.items[1].RecipientUserID != "alice" {
		t.Fatalf("notifications: %+v", f.notifications.items)
	}
}

func TestTaskDeliveryAccessAndWithdrawal(t *testing.T) {
	f := newDeliveryFixture(t)
	if _, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.user("manager"), ReviewerUserID: "alice", Body: "submitted on behalf of alice"}); !errors.Is(err, ErrInvalidDeliveryReviewer) {
		t.Fatalf("delegated self-review: %v", err)
	}
	for _, user := range []string{"viewer", "charlie", "outsider"} {
		if _, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.user(user), ReviewerUserID: "bob", Body: "unauthorized"}); err == nil {
			t.Fatalf("%s submitted", user)
		}
	}
	for _, reviewer := range []string{"viewer", "outsider", "alice"} {
		if _, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: reviewer, Body: "output"}); !errors.Is(err, ErrInvalidDeliveryReviewer) {
			t.Fatalf("reviewer %s: %v", reviewer, err)
		}
	}
	first, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: "bob", Body: "output"})
	if err != nil {
		t.Fatal(err)
	}
	otherTenant := f.input
	otherTenant.Identity.TenantID, otherTenant.Identity.Role = "tenant-b", auth.RoleAdmin
	if _, err := f.service.GetDelivery(f.ctx, otherTenant, first.ID); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("cross-tenant read: %v", err)
	}
	if _, err := f.service.ReviewDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: otherTenant, DeliveryID: first.ID, Decision: DeliveryApproved}); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("cross-tenant review: %v", err)
	}
	otherTask := f.input
	otherTask.TaskID = uuid.NewString()
	if _, err := f.service.GetDelivery(f.ctx, otherTask, first.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-task read: %v", err)
	}
	if _, err := f.service.WithdrawDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.user("charlie"), DeliveryID: first.ID}); !errors.Is(err, project.ErrForbidden) {
		t.Fatalf("unrelated withdrawal: %v", err)
	}
	if err := f.projects.DeleteMember(f.ctx, "tenant-a", f.input.ProjectID, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ReviewDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.user("bob"), DeliveryID: first.ID, Decision: DeliveryApproved}); err == nil {
		t.Fatal("removed reviewer approved")
	}
	withdrawn, err := f.service.WithdrawDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.user("manager"), DeliveryID: first.ID, Note: "بررسی‌کننده تغییر می‌کند"})
	if err != nil || withdrawn.Status != DeliveryWithdrawn {
		t.Fatalf("manager withdrawal: %+v %v", withdrawn, err)
	}
	if _, err := f.service.WithdrawDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.input, DeliveryID: first.ID}); !errors.Is(err, ErrDeliveryResolved) {
		t.Fatalf("repeated withdrawal: %v", err)
	}
	second, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: "charlie", Body: "output 2"})
	if err != nil || second.Revision != 2 {
		t.Fatalf("replacement reviewer: %+v %v", second, err)
	}
	if _, err := f.service.WithdrawDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.input, DeliveryID: second.ID}); err != nil {
		t.Fatalf("submitter withdrawal: %v", err)
	}
	var invalid int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_deliveries WHERE reviewer_user_id = submitted_by_user_id`).Scan(&invalid); err != nil || invalid != 0 {
		t.Fatal("self review rows persisted")
	}
}

func TestConcurrentDeliveriesAndReviews(t *testing.T) {
	f := newDeliveryFixture(t)
	start := make(chan struct{})
	results := make([]Delivery, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: "bob", Body: "output"})
		}(i)
	}
	close(start)
	wg.Wait()
	var id string
	var successes, conflicts int
	for i, err := range errs {
		if err == nil {
			successes++
			id = results[i].ID
		} else if errors.Is(err, ErrDeliveryPending) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("submissions: %d successes, %d conflicts", successes, conflicts)
	}
	start = make(chan struct{})
	for i, decision := range []DeliveryStatus{DeliveryApproved, DeliveryChangesRequested} {
		wg.Add(1)
		go func(i int, decision DeliveryStatus) {
			defer wg.Done()
			<-start
			_, errs[i] = f.service.ReviewDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.user("bob"), DeliveryID: id, Decision: decision, Note: "review"})
		}(i, decision)
	}
	close(start)
	wg.Wait()
	successes, conflicts = 0, 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrDeliveryResolved) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("reviews: %d successes, %d conflicts", successes, conflicts)
	}
}

func TestDeliveryArchiveAndAuditRollback(t *testing.T) {
	f := newDeliveryFixture(t)
	if _, err := f.pool.Exec(f.ctx, `ALTER TABLE audit_logs RENAME TO hidden_audit_logs`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: "bob", Body: "output"}); err == nil {
		t.Fatal("submission survived audit failure")
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_deliveries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial submission: %d %v", count, err)
	}
	if _, err := f.pool.Exec(f.ctx, `ALTER TABLE hidden_audit_logs RENAME TO audit_logs`); err != nil {
		t.Fatal(err)
	}
	delivery, err := f.service.SubmitDelivery(f.ctx, SubmitDeliveryInput{DeliveryInput: f.input, ReviewerUserID: "bob", Body: "output"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.projects.Archive(f.ctx, "tenant-a", f.input.ProjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ReviewDelivery(f.ctx, ReviewDeliveryInput{DeliveryInput: f.user("bob"), DeliveryID: delivery.ID, Decision: DeliveryApproved}); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("archived project review: %v", err)
	}
	if _, err := f.service.GetDelivery(f.ctx, f.input, delivery.ID); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("archived project read: %v", err)
	}
}
