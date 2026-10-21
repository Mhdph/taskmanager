package task

import (
	"context"
	"time"
)

// EventKind identifies the task change that automation rules react to.
type EventKind string

const (
	EventTaskCreated       EventKind = "task.created"
	EventTaskStatusChanged EventKind = "task.status_changed"
	EventTaskAssigned      EventKind = "task.assigned"
	EventTaskDue           EventKind = "task.due"
)

// Event carries the task state automation rules need. It is defined in the
// task package so the task service can publish it without depending on the
// automation package; automation rules implement AutomationNotifier.
type Event struct {
	Kind EventKind

	TenantID  string
	ProjectID string
	TaskID    string

	// ActorUserID is the user who caused the change. It is empty for
	// events produced by a background sweep.
	ActorUserID string

	Title string

	AssigneeUserID  *string
	CreatedByUserID string

	FromStatusID   *string
	FromStatusName string
	ToStatusID     *string
	ToStatusName   string

	DueAt *time.Time
}

// AutomationNotifier receives task events that automation rules can act on.
// Implementations must not block the task write that emitted the event.
type AutomationNotifier interface {
	HandleTaskEvent(ctx context.Context, event Event)
}

// SetAutomationNotifier wires the automation engine after construction: the
// automation service needs this task service to apply assignment actions, so
// the two cannot receive each other in their constructors.
func (s *Service) SetAutomationNotifier(notifier AutomationNotifier) {
	s.automationNotifier = notifier
}

func (s *Service) notifyAutomation(
	ctx context.Context,
	event Event,
) {
	if s.automationNotifier == nil {
		return
	}

	s.automationNotifier.HandleTaskEvent(ctx, event)
}

// automationEventForTask builds the common part of a task event.
func automationEventForTask(
	kind EventKind,
	identityUserID string,
	item Task,
) Event {
	return Event{
		Kind: kind,

		TenantID:  item.TenantID,
		ProjectID: item.ProjectID,
		TaskID:    item.ID,

		ActorUserID: identityUserID,

		Title: item.Title,

		AssigneeUserID:  item.AssigneeUserID,
		CreatedByUserID: item.CreatedByUserID,

		DueAt: item.DueAt,
	}
}
