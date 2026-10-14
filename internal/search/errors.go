package search

import "errors"

var (
	ErrInvalidQuery = errors.New(
		"invalid search query",
	)

	ErrInvalidType = errors.New(
		"invalid search type",
	)
)
