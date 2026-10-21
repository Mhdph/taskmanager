package automation

import (
	"context"
	"testing"
	"time"

	"github.com/mhdph/taskmanager/internal/task"
)

func stringPointer(value string) *string { return &value }

func boolPointer(value bool) *bool { return &value }

func TestRuleMatchesStatusChange(t *testing.T) {
	from := "11111111-1111-1111-1111-111111111111"
	to := "22222222-2222-2222-2222-222222222222"
	other := "33333333-3333-3333-3333-333333333333"

	tests := []struct {
		name  string
		rule  Rule
		event task.Event
		want  bool
	}{
		{
			name: "any status change",
			rule: Rule{
				TriggerType: TriggerTaskStatusChanged,
			},
			event: task.Event{
				FromStatusID: stringPointer(from),
				ToStatusID:   stringPointer(to),
			},
			want: true,
		},
		{
			name: "matching target status",
			rule: Rule{
				TriggerType:   TriggerTaskStatusChanged,
				TriggerConfig: Trigger{ToStatusID: &to},
			},
			event: task.Event{
				FromStatusID: stringPointer(from),
				ToStatusID:   stringPointer(to),
			},
			want: true,
		},
		{
			name: "different target status",
			rule: Rule{
				TriggerType:   TriggerTaskStatusChanged,
				TriggerConfig: Trigger{ToStatusID: &other},
			},
			event: task.Event{
				FromStatusID: stringPointer(from),
				ToStatusID:   stringPointer(to),
			},
			want: false,
		},
		{
			name: "matching source and target",
			rule: Rule{
				TriggerType: TriggerTaskStatusChanged,
				TriggerConfig: Trigger{
					FromStatusID: &from,
					ToStatusID:   &to,
				},
			},
			event: task.Event{
				FromStatusID: stringPointer(from),
				ToStatusID:   stringPointer(to),
			},
			want: true,
		},
		{
			name: "missing event status",
			rule: Rule{
				TriggerType:   TriggerTaskStatusChanged,
				TriggerConfig: Trigger{ToStatusID: &to},
			},
			event: task.Event{},
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.rule.matches(test.event); got != test.want {
				t.Fatalf("matches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRuleMatchesTaskCreated(t *testing.T) {
	assigned := "alice"

	tests := []struct {
		name  string
		rule  Rule
		event task.Event
		want  bool
	}{
		{
			name:  "any created task",
			rule:  Rule{TriggerType: TriggerTaskCreated},
			event: task.Event{},
			want:  true,
		},
		{
			name: "created with assignee",
			rule: Rule{
				TriggerType:   TriggerTaskCreated,
				TriggerConfig: Trigger{HasAssignee: boolPointer(true)},
			},
			event: task.Event{AssigneeUserID: &assigned},
			want:  true,
		},
		{
			name: "created without assignee does not match",
			rule: Rule{
				TriggerType:   TriggerTaskCreated,
				TriggerConfig: Trigger{HasAssignee: boolPointer(true)},
			},
			event: task.Event{},
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.rule.matches(test.event); got != test.want {
				t.Fatalf("matches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRuleMatchesTaskAssigned(t *testing.T) {
	alice := "alice"
	bob := "bob"

	rule := Rule{
		TriggerType:   TriggerTaskAssigned,
		TriggerConfig: Trigger{ToUserID: &bob},
	}

	if !rule.matches(task.Event{AssigneeUserID: &bob}) {
		t.Fatal("expected assignment to bob to match")
	}

	if rule.matches(task.Event{AssigneeUserID: &alice}) {
		t.Fatal("expected assignment to alice not to match")
	}

	if rule.matches(task.Event{}) {
		t.Fatal("expected unassigned task not to match")
	}
}

func TestTriggerForEvent(t *testing.T) {
	tests := map[task.EventKind]TriggerType{
		task.EventTaskCreated:       TriggerTaskCreated,
		task.EventTaskStatusChanged: TriggerTaskStatusChanged,
		task.EventTaskAssigned:      TriggerTaskAssigned,
		task.EventTaskDue:           TriggerTaskDue,
	}

	for kind, want := range tests {
		if got := triggerForEvent(kind); got != want {
			t.Fatalf(
				"triggerForEvent(%q) = %q, want %q",
				kind,
				got,
				want,
			)
		}
	}

	if got := triggerForEvent("unknown"); got != "" {
		t.Fatalf("unknown event mapped to %q", got)
	}
}

func TestTriggerNormalize(t *testing.T) {
	status := "11111111-1111-1111-1111-111111111111"
	user := "bob"
	days := 2
	flag := true

	normalized := Trigger{
		FromStatusID: &status,
		ToStatusID:   &status,
		HasAssignee:  &flag,
		ToUserID:     &user,
		DaysBefore:   &days,
	}.normalize(TriggerTaskDue)

	if normalized.DaysBefore == nil || *normalized.DaysBefore != days {
		t.Fatal("due trigger lost daysBefore")
	}

	if normalized.FromStatusID != nil ||
		normalized.ToStatusID != nil ||
		normalized.HasAssignee != nil ||
		normalized.ToUserID != nil {
		t.Fatal("due trigger kept fields of another trigger type")
	}
}

func TestValidateActionWithoutDatabase(t *testing.T) {
	ctx := context.Background()

	service := &Service{}

	creator, err := service.validateAction(
		ctx,
		"tenant-a",
		"11111111-1111-1111-1111-111111111111",
		Action{
			Type:     ActionSetAssignee,
			Assignee: AssigneeCreator,
			UserID:   stringPointer("ignored"),
		},
	)
	if err != nil {
		t.Fatalf("validate creator assignment: %v", err)
	}

	if creator.Assignee != AssigneeCreator || creator.UserID != nil {
		t.Fatalf("creator action was not normalized: %+v", creator)
	}

	notify, err := service.validateAction(
		ctx,
		"tenant-a",
		"11111111-1111-1111-1111-111111111111",
		Action{
			Type:       ActionNotify,
			Recipients: []Recipient{RecipientAssignee, RecipientWatchers},
			Title:      "Heads up",
		},
	)
	if err != nil {
		t.Fatalf("validate notification: %v", err)
	}

	if len(notify.Recipients) != 2 || notify.Title != "Heads up" {
		t.Fatalf("unexpected notification action: %+v", notify)
	}

	if _, err := service.validateAction(
		ctx,
		"tenant-a",
		"11111111-1111-1111-1111-111111111111",
		Action{Type: ActionNotify},
	); err == nil {
		t.Fatal("expected notification without recipients to fail")
	}

	if _, err := service.validateAction(
		ctx,
		"tenant-a",
		"11111111-1111-1111-1111-111111111111",
		Action{
			Type:     ActionSetAssignee,
			Assignee: AssigneeUser,
		},
	); err == nil {
		t.Fatal("expected user assignment without userId to fail")
	}
}

func TestNotificationBody(t *testing.T) {
	dueAt := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	statusRule := Rule{TriggerType: TriggerTaskStatusChanged}
	statusEvent := task.Event{
		Title:          "Fix login",
		FromStatusName: "todo",
		ToStatusName:   "doing",
	}

	if got := notificationBody(statusRule, statusEvent); got != "Fix login: todo → doing" {
		t.Fatalf("unexpected status body %q", got)
	}

	dueRule := Rule{TriggerType: TriggerTaskDue}
	dueEvent := task.Event{
		Title: "Fix login",
		DueAt: &dueAt,
	}

	if got := notificationBody(dueRule, dueEvent); got != "Fix login is due on 2026-10-24" {
		t.Fatalf("unexpected due body %q", got)
	}
}
