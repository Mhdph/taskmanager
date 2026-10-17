package team

import "time"

// Team is a workspace-level group. Members belong to one primary team
// (exposed on the member directory) and projects may be owned by a team
// (exposed on the project).
type Team struct {
	ID string

	TenantID string

	Name string

	Icon  string
	Color string

	Description string

	CreatedByUserID string

	CreatedAt time.Time
	UpdatedAt time.Time
}
