package project

import "time"

type Project struct {
	ID              string
	TenantID        string
	Name            string
	Description     *string
	CreatedByUserID string

	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}
type ListOptions struct {
	Limit  int32
	Cursor *Cursor
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
