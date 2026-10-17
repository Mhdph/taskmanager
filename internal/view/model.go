package view

import (
	"time"
)

// SavedView is a named task view attached to a project. The filters are
// stored exactly as produced by the client (validated for shape), so the
// view engine can re-apply them verbatim.
type SavedView struct {
	ID string

	TenantID  string
	ProjectID string

	Name string

	// Layout: list | board | table.
	ViewType string

	Filters []Filter

	CreatedByUserID string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Filter is one applied filter condition. Field whitelist lives in the
// service; values are opaque strings (option ids). The JSON keys match the
// wire format the client produced ({f, op, v}).
type Filter struct {
	Field string `json:"f"`

	// Operator: is (default) or not.
	Operator string `json:"op,omitempty"`

	Values []string `json:"v"`
}
