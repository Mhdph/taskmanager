package view

import "errors"

var (
	ErrViewNotFound = errors.New(
		"saved view not found",
	)

	ErrViewNameRequired = errors.New(
		"view name is required",
	)

	ErrViewInvalidType = errors.New(
		"invalid view type",
	)

	ErrViewFiltersTooMany = errors.New(
		"too many filters",
	)

	ErrViewInvalidFilter = errors.New(
		"invalid filter",
	)

	ErrNothingToUpdate = errors.New(
		"no changes requested",
	)

	// ErrViewConflict is returned when another view already uses the
	// requested name in the same project.
	ErrViewConflict = errors.New(
		"view name already exists",
	)
)
