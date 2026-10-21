package automation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

const automationTestTenant = "tenant-automation"

type recordingSender struct {
	mu     sync.Mutex
	inputs []notification.SendInput
}

func (s *recordingSender) Send(
	_ context.Context,
	input notification.SendInput,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.inputs = append(s.inputs, input)

	return nil
}

func (s *recordingSender) snapshot() []notification.SendInput {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]notification.SendInput, len(s.inputs))
	copy(result, s.inputs)

	return result
}

func (s *recordingSender) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.inputs = nil
}

type fakeUpdater struct {
	mu    sync.Mutex
	calls []task.UpdateInput
}

type recordingNotifier struct {
	mu     sync.Mutex
	events []task.Event
}

func (n *recordingNotifier) HandleTaskEvent(
	_ context.Context,
	event task.Event,
) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.events = append(n.events, event)
}

func (n *recordingNotifier) snapshot() []task.Event {
	n.mu.Lock()
	defer n.mu.Unlock()

	result := make([]task.Event, len(n.events))
	copy(result, n.events)

	return result
}

func (u *fakeUpdater) Update(
	_ context.Context,
	input task.UpdateInput,
) (task.Task, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.calls = append(u.calls, input)

	return task.Task{
		ID:        input.TaskID,
		ProjectID: input.ProjectID,

		AssigneeUserID: input.AssigneeUserID,
	}, nil
}

func (u *fakeUpdater) snapshot() []task.UpdateInput {
	u.mu.Lock()
	defer u.mu.Unlock()

	result := make([]task.UpdateInput, len(u.calls))
	copy(result, u.calls)

	return result
}

// integrationAutomationService creates an isolated schema with the
// application migrations applied and a project with two statuses, a manager,
// a member and one task.
func integrationAutomationService(
	t *testing.T,
) (*Service, *fakeUpdater, *recordingSender, *pgxpool.Pool, context.Context, automationFixture) {
	t.Helper()

	url := os.Getenv("TASKMANAGER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TASKMANAGER_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		45*time.Second,
	)
	t.Cleanup(cancel)

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}

	schema := "test_" + uuid.NewString()[0:8]

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	if _, err := admin.Exec(
		ctx,
		`CREATE SCHEMA `+schema,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(
			context.Background(),
			`DROP SCHEMA `+schema+` CASCADE`,
		)
	})

	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	files, err := filepath.Glob("../../db/migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)

	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}

	queries := db.New(pool)

	fixture := automationFixture{
		ProjectID: uuid.NewString(),
		TodoID:    uuid.NewString(),
		DoingID:   uuid.NewString(),
		TaskID:    uuid.NewString(),
		DueAt:     time.Now().UTC().Add(2 * time.Hour),
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO projects (id, tenant_id, name, created_by_user_id)
		 VALUES ($1, $2, 'automation project', 'alice')`,
		fixture.ProjectID,
		automationTestTenant,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO project_statuses (id, tenant_id, project_id, name, position, category)
		 VALUES ($1, $2, $3, 'todo', 0, 'todo'),
		        ($4, $2, $3, 'doing', 1, 'in_progress')`,
		fixture.TodoID,
		automationTestTenant,
		fixture.ProjectID,
		fixture.DoingID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO project_members (project_id, tenant_id, user_id, role)
		 VALUES ($1, $2, 'alice', 'manager'),
		        ($1, $2, 'bob', 'member')`,
		fixture.ProjectID,
		automationTestTenant,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tasks (
			id, tenant_id, project_id, status_id, title,
			created_by_user_id, due_at
		 ) VALUES ($1, $2, $3, $4, 'Review the release', 'alice', $5)`,
		fixture.TaskID,
		automationTestTenant,
		fixture.ProjectID,
		fixture.TodoID,
		fixture.DueAt,
	); err != nil {
		t.Fatal(err)
	}

	projectRepository := project.NewRepository(pool, db.New(pool))

	sender := &recordingSender{}
	updater := &fakeUpdater{}

	service := NewService(
		NewRepository(pool, queries),
		task.NewRepository(pool, queries),
		projectRepository,
		project.NewAuthorizer(projectRepository),
		sender,
		20,
	)
	service.SetTaskUpdater(updater)

	return service, updater, sender, pool, ctx, fixture
}

type automationFixture struct {
	ProjectID string
	TodoID    string
	DoingID   string
	TaskID    string
	DueAt     time.Time
}

func automationIdentity(userID string) auth.Identity {
	return auth.Identity{
		TenantID: automationTestTenant,
		UserID:   userID,
		Role:     auth.RoleAdmin,
	}
}

func TestAutomationStatusRuleAssignsAndNotifies(t *testing.T) {
	service, updater, sender, _, ctx, fixture :=
		integrationAutomationService(t)

	bob := "bob"

	rule, err := service.Create(
		ctx,
		CreateInput{
			Identity:  automationIdentity("alice"),
			ProjectID: fixture.ProjectID,
			Name:      "Assign reviewer",

			TriggerType: TriggerTaskStatusChanged,
			TriggerConfig: Trigger{
				ToStatusID: &fixture.DoingID,
			},

			Actions: []Action{
				{
					Type:     ActionSetAssignee,
					Assignee: AssigneeUser,
					UserID:   &bob,
				},
				{
					Type:       ActionNotify,
					Recipients: []Recipient{RecipientAssignee},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if !rule.Enabled {
		t.Fatal("new rule should be enabled")
	}

	from := fixture.TodoID
	to := fixture.DoingID

	service.handleEvent(
		ctx,
		task.Event{
			Kind: task.EventTaskStatusChanged,

			TenantID:  automationTestTenant,
			ProjectID: fixture.ProjectID,
			TaskID:    fixture.TaskID,

			ActorUserID: "alice",

			Title: "Review the release",

			CreatedByUserID: "alice",

			FromStatusID:   &from,
			FromStatusName: "todo",
			ToStatusID:     &to,
			ToStatusName:   "doing",
		},
	)

	calls := updater.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected one assignment, got %d", len(calls))
	}

	if calls[0].AssigneeUserID == nil || *calls[0].AssigneeUserID != bob {
		t.Fatalf("unexpected assignee: %+v", calls[0].AssigneeUserID)
	}

	if !calls[0].SuppressAutomation {
		t.Fatal("rule assignment must suppress another automation pass")
	}

	messages := sender.snapshot()
	if len(messages) != 1 {
		t.Fatalf("expected one notification, got %d", len(messages))
	}

	if messages[0].RecipientUserID != bob {
		t.Fatalf(
			"unexpected notification recipient %q",
			messages[0].RecipientUserID,
		)
	}

	if messages[0].Type != notification.EventAutomation {
		t.Fatalf("unexpected notification type %q", messages[0].Type)
	}

	if messages[0].ActorUserID == nil ||
		*messages[0].ActorUserID != "alice" {
		t.Fatal("notification should carry the acting user")
	}
}

func TestAutomationStatusRuleIgnoresOtherStatus(t *testing.T) {
	service, updater, _, _, ctx, fixture :=
		integrationAutomationService(t)

	bob := "bob"

	if _, err := service.Create(
		ctx,
		CreateInput{
			Identity:  automationIdentity("alice"),
			ProjectID: fixture.ProjectID,
			Name:      "Only when doing",

			TriggerType: TriggerTaskStatusChanged,
			TriggerConfig: Trigger{
				ToStatusID: &fixture.DoingID,
			},

			Actions: []Action{
				{
					Type:     ActionSetAssignee,
					Assignee: AssigneeUser,
					UserID:   &bob,
				},
			},
		},
	); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	from := fixture.DoingID
	to := fixture.TodoID

	service.handleEvent(
		ctx,
		task.Event{
			Kind: task.EventTaskStatusChanged,

			TenantID:  automationTestTenant,
			ProjectID: fixture.ProjectID,
			TaskID:    fixture.TaskID,

			ActorUserID: "alice",

			FromStatusID: &from,
			ToStatusID:   &to,
		},
	)

	if len(updater.snapshot()) != 0 {
		t.Fatal("rule applied to a status change it does not match")
	}
}

func TestAutomationDueRuleNotifiesOnce(t *testing.T) {
	service, _, sender, _, ctx, fixture :=
		integrationAutomationService(t)

	days := 1

	rule, err := service.Create(
		ctx,
		CreateInput{
			Identity:  automationIdentity("alice"),
			ProjectID: fixture.ProjectID,
			Name:      "Due soon",

			TriggerType: TriggerTaskDue,
			TriggerConfig: Trigger{
				DaysBefore: &days,
			},

			Actions: []Action{
				{
					Type:    ActionNotify,
					UserIDs: []string{"bob"},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	service.sweepDue(ctx)

	messages := sender.snapshot()
	if len(messages) != 1 {
		t.Fatalf("expected one due notification, got %d", len(messages))
	}

	if messages[0].Data["automationRuleId"] != rule.ID {
		t.Fatalf("notification is missing the rule id: %+v", messages[0].Data)
	}

	// The deadline was already reminded; a second sweep must not repeat it.
	service.sweepDue(ctx)

	if got := len(sender.snapshot()); got != 1 {
		t.Fatalf("expected the reminder to be claimed once, got %d", got)
	}
}

func TestTaskServicePublishesAutomationEvents(t *testing.T) {
	service, _, sender, pool, ctx, fixture :=
		integrationAutomationService(t)

	queries := db.New(pool)

	taskRepository := task.NewRepository(pool, queries)
	projectRepository := project.NewRepository(pool, queries)

	taskService := task.NewService(
		taskRepository,
		projectRepository,
		project.NewAuthorizer(projectRepository),
		sender,
		nil,
		nil,
		25*1024*1024,
	)

	notifier := &recordingNotifier{}
	taskService.SetAutomationNotifier(notifier)

	// The rule applies its action through the real task service, which is
	// also the event source. This is the exact wiring main.go uses.
	service.SetTaskUpdater(taskService)

	bob := "bob"

	if _, err := service.Create(
		ctx,
		CreateInput{
			Identity:  automationIdentity("alice"),
			ProjectID: fixture.ProjectID,
			Name:      "Assign reviewer",

			TriggerType: TriggerTaskStatusChanged,
			TriggerConfig: Trigger{
				ToStatusID: &fixture.DoingID,
			},

			Actions: []Action{
				{
					Type:     ActionSetAssignee,
					Assignee: AssigneeUser,
					UserID:   &bob,
				},
			},
		},
	); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	doing := fixture.DoingID

	if _, err := taskService.Update(
		ctx,
		task.UpdateInput{
			Identity:  automationIdentity("alice"),
			ProjectID: fixture.ProjectID,
			TaskID:    fixture.TaskID,

			StatusID: &doing,
		},
	); err != nil {
		t.Fatalf("change status: %v", err)
	}

	events := notifier.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected one task event, got %d", len(events))
	}

	event := events[0]

	if event.Kind != task.EventTaskStatusChanged {
		t.Fatalf("unexpected event kind %q", event.Kind)
	}

	if event.FromStatusID == nil ||
		*event.FromStatusID != fixture.TodoID {
		t.Fatal("event is missing the previous status")
	}

	if event.ToStatusID == nil ||
		*event.ToStatusID != fixture.DoingID {
		t.Fatal("event is missing the next status")
	}

	// Evaluate the captured event the way the worker would.
	service.handleEvent(ctx, event)

	updated, err := taskRepository.GetForProject(
		ctx,
		automationTestTenant,
		fixture.ProjectID,
		fixture.TaskID,
	)
	if err != nil {
		t.Fatalf("read task: %v", err)
	}

	if updated.AssigneeUserID == nil ||
		*updated.AssigneeUserID != bob {
		t.Fatalf(
			"rule did not assign the task: %+v",
			updated.AssigneeUserID,
		)
	}

	// The assignment was applied with SuppressAutomation, so it must not
	// have produced another event.
	if got := len(notifier.snapshot()); got != 1 {
		t.Fatalf("rule action cascaded into %d events", got)
	}

	// An explicitly suppressed update publishes nothing either.
	todo := fixture.TodoID

	if _, err := taskService.Update(
		ctx,
		task.UpdateInput{
			Identity:  automationIdentity("alice"),
			ProjectID: fixture.ProjectID,
			TaskID:    fixture.TaskID,

			StatusID:           &todo,
			SuppressAutomation: true,
		},
	); err != nil {
		t.Fatalf("suppressed status change: %v", err)
	}

	if got := len(notifier.snapshot()); got != 1 {
		t.Fatalf("suppressed update published %d events", got-1)
	}
}

func TestAutomationRuleLimit(t *testing.T) {
	service, _, _, _, ctx, fixture :=
		integrationAutomationService(t)

	bob := "bob"

	for i := 0; i < 20; i++ {
		if _, err := service.Create(
			ctx,
			CreateInput{
				Identity:  automationIdentity("alice"),
				ProjectID: fixture.ProjectID,
				Name:      "Rule",

				TriggerType: TriggerTaskCreated,

				Actions: []Action{
					{
						Type:     ActionSetAssignee,
						Assignee: AssigneeUser,
						UserID:   &bob,
					},
				},
			},
		); err != nil {
			t.Fatalf("create rule %d: %v", i, err)
		}
	}

	if _, err := service.Create(
		ctx,
		CreateInput{
			Identity:    automationIdentity("alice"),
			ProjectID:   fixture.ProjectID,
			Name:        "One too many",
			TriggerType: TriggerTaskCreated,
			Actions: []Action{
				{
					Type:     ActionSetAssignee,
					Assignee: AssigneeCreator,
				},
			},
		},
	); !errors.Is(err, ErrTooManyRules) {
		t.Fatalf("expected ErrTooManyRules, got %v", err)
	}
}
