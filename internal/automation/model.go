package automation

import (
	"encoding/json"
	"strings"
	"time"
)

// TriggerType is the event that starts a rule.
type TriggerType string

const (
	TriggerTaskStatusChanged TriggerType = "task_status_changed"
	TriggerTaskCreated       TriggerType = "task_created"
	TriggerTaskAssigned      TriggerType = "task_assigned"
	TriggerTaskDue           TriggerType = "task_due"
)

func (t TriggerType) Valid() bool {
	switch t {
	case TriggerTaskStatusChanged,
		TriggerTaskCreated,
		TriggerTaskAssigned,
		TriggerTaskDue:
		return true
	default:
		return false
	}
}

// Trigger configures the filter a rule applies to its trigger. Only the
// fields that belong to the rule's trigger type are meaningful; the service
// clears the others before storing the rule.
type Trigger struct {
	// FromStatusID and ToStatusID filter a status change. A nil value
	// matches any status.
	FromStatusID *string `json:"fromStatusId,omitempty"`
	ToStatusID   *string `json:"toStatusId,omitempty"`

	// HasAssignee filters task creation by whether the task started with an
	// assignee. Nil matches both.
	HasAssignee *bool `json:"hasAssignee,omitempty"`

	// ToUserID filters assignments by the new assignee.
	ToUserID *string `json:"toUserId,omitempty"`

	// DaysBefore is how far ahead the due-date sweep looks. Nil means one
	// day.
	DaysBefore *int `json:"daysBefore,omitempty"`
}

// ActionType is one effect a rule can apply.
type ActionType string

const (
	ActionSetAssignee ActionType = "set_assignee"
	ActionNotify      ActionType = "notify"
)

// AssigneeTarget selects where a set_assignee action takes its user from.
type AssigneeTarget string

const (
	AssigneeUser    AssigneeTarget = "user"
	AssigneeCreator AssigneeTarget = "creator"
)

// Recipient names a dynamic notification audience.
type Recipient string

const (
	RecipientAssignee Recipient = "assignee"
	RecipientCreator  Recipient = "creator"
	RecipientWatchers Recipient = "watchers"
)

const (
	// MaxActionsPerRule caps the ordered action list of one rule.
	MaxActionsPerRule = 5

	// DefaultDueDaysBefore is the sweep window used when a due rule does
	// not set one.
	DefaultDueDaysBefore = 1

	// MaxDueDaysBefore caps the due sweep window.
	MaxDueDaysBefore = 30

	// maxNameLength caps the rule name.
	maxNameLength = 120

	// maxNotificationTitleLength caps an action title.
	maxNotificationTitleLength = 200

	// maxNotificationBodyLength caps an action body.
	maxNotificationBodyLength = 2000
)

// Action is one effect of a rule. The fields used depend on Type.
type Action struct {
	Type ActionType `json:"type"`

	// set_assignee: "user" assigns UserID, "creator" assigns the task
	// creator.
	Assignee AssigneeTarget `json:"assignee,omitempty"`
	UserID   *string        `json:"userId,omitempty"`

	// notify audiences: any of assignee, creator and watchers, plus
	// explicit member ids.
	Recipients []Recipient `json:"recipients,omitempty"`
	UserIDs    []string    `json:"userIds,omitempty"`

	// Optional notification text; defaults are generated when empty.
	Title string  `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}

// Rule is one stored automation rule.
type Rule struct {
	ID        string
	TenantID  string
	ProjectID string
	Name      string

	TriggerType   TriggerType
	TriggerConfig Trigger

	Actions []Action

	Enabled bool

	CreatedByUserID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// normalize clears trigger fields that do not belong to the trigger type,
// so stored rules cannot carry contradictory configuration.
func (t Trigger) normalize(triggerType TriggerType) Trigger {
	switch triggerType {
	case TriggerTaskStatusChanged:
		return Trigger{
			FromStatusID: t.FromStatusID,
			ToStatusID:   t.ToStatusID,
		}
	case TriggerTaskCreated:
		return Trigger{HasAssignee: t.HasAssignee}
	case TriggerTaskAssigned:
		return Trigger{ToUserID: t.ToUserID}
	case TriggerTaskDue:
		return Trigger{DaysBefore: t.DaysBefore}
	default:
		return Trigger{}
	}
}

func (t Trigger) encode() ([]byte, error) {
	return json.Marshal(t)
}

func decodeTrigger(raw []byte) (Trigger, error) {
	var trigger Trigger
	if len(raw) == 0 {
		return trigger, nil
	}
	if err := json.Unmarshal(raw, &trigger); err != nil {
		return Trigger{}, err
	}
	return trigger, nil
}

func encodeActions(actions []Action) ([]byte, error) {
	return json.Marshal(actions)
}

func decodeActions(raw []byte) ([]Action, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var actions []Action
	if err := json.Unmarshal(raw, &actions); err != nil {
		return nil, err
	}
	return actions, nil
}

func normalizeName(name string) string {
	return strings.TrimSpace(name)
}
