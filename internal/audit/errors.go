package audit

import "errors"

var (
	ErrInvalidFilter = errors.New(
		"invalid filter",
	)

	ErrInvalidCursor = errors.New(
		"invalid cursor",
	)
)
