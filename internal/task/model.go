package task

import "time"

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

type Task struct {
	ID        string
	TenantID  string
	ProjectID string
	StatusID  string

	ParentTaskID *string

	Title       string
	Description *string

	Priority Priority

	AssigneeUserID *string

	CreatedByUserID string

	DueAt       *time.Time
	CompletedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
}
