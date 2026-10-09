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
