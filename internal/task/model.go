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

type Cursor struct {
	CreatedAt time.Time
	ID        string
}

type ListOptions struct {
	Limit  int32
	Cursor *Cursor

	StatusID       *string
	AssigneeUserID *string
	Priority       *Priority
	ParentTaskID   *string
	Search         *string
}

type TaskPage struct {
	Items      []Task
	NextCursor *string
}

type Dependency struct {
	TaskID          string
	DependsOnTaskID string
	CreatedAt       time.Time
}

type Watcher struct {
	TaskID    string
	UserID    string
	CreatedAt time.Time
}
type Attachment struct {
	ID string

	TaskID string

	Filename string

	ContentType string

	SizeBytes int64

	ObjectKey string

	UploadedByUserID string

	CreatedAt time.Time
}
