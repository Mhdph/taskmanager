package member

import "time"

type Member struct {
	TenantID  string
	UserID    string
	Email     string
	FirstName string
	LastName  string
	Role      Role
	Title     string
	Status    Status

	// TeamID is the member's primary team; nil when no team.
	TeamID *string

	CreatedAt time.Time
	UpdatedAt time.Time
}

type ListOptions struct {
	Role   *Role
	Status *Status
	Search *string

	Limit int32
}
