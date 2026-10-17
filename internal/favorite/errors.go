package favorite

import "errors"

var (
	ErrNotFound = errors.New(
		"favorite not found",
	)

	ErrInvalidID = errors.New(
		"invalid id",
	)
)
