package ai

import "errors"

var (
	ErrInvalidInput = errors.New(
		"invalid ai input",
	)

	ErrProviderUnavailable = errors.New(
		"ai provider unavailable",
	)
)
