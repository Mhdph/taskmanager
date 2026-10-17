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

	StartAt *time.Time
	DueAt   *time.Time

	CompletedAt *time.Time

	// Board ordering within the status column; set by the reorder endpoint
	// and defaulted at creation.
	Position        *float64
	EstimateSeconds *int64

	// Recurrence rule: daily | weekly | biweekly | monthly.
	Recurrence *string

	// Labels attached to the task; populated on detail reads and in the
	// tenant-wide listing, not by every mutation response.
	Labels []Label

	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}

type Cursor struct {
	CreatedAt time.Time
	ID        string
}

type ListOptions struct {
	Archived bool
	Limit    int32
	Cursor   *Cursor

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

type MyWorkOptions struct {
	IsAdmin bool

	Limit int32

	Cursor *Cursor

	StatusID *string

	Priority *Priority

	DueFrom *time.Time
	DueTo   *time.Time

	OverdueOnly bool
}

type MyWorkPage struct {
	Items []Task

	NextCursor *string
}
