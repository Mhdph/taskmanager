package project

import "time"

type Project struct {
	ID              string
	TenantID        string
	Name            string
	Description     *string
	CreatedByUserID string

	// TeamID is the owning workspace team; nil when unassigned.
	TeamID *string

	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}
type ListOptions struct {
	Archived bool
	Limit    int32
	Cursor   *Cursor
}

type ProjectPage struct {
	Items      []Project
	NextCursor *string
}

type ProjectMember struct {
	ProjectID string
	TenantID  string
	UserID    string
	Role      MemberRole

	CreatedAt time.Time
}
type StatusCategory string

const (
	StatusCategoryTodo       StatusCategory = "todo"
	StatusCategoryInProgress StatusCategory = "in_progress"
	StatusCategoryDone       StatusCategory = "done"
)

type ProjectStatus struct {
	ID        string
	TenantID  string
	ProjectID string

	Name     string
	Position int32
	Category StatusCategory

	CreatedAt time.Time
}
