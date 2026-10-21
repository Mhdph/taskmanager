package automation

import "errors"

var (
	ErrInvalidID = errors.New("invalid automation rule id")

	ErrRuleNotFound = errors.New("automation rule not found")

	ErrInvalidName = errors.New("invalid automation rule name")

	ErrInvalidTrigger = errors.New(
		"invalid automation trigger",
	)

	ErrInvalidTriggerConfig = errors.New(
		"invalid automation trigger config",
	)

	ErrInvalidAction = errors.New(
		"invalid automation action",
	)

	// ErrTooManyRules is returned when creating a rule would exceed the
	// per-project limit.
	ErrTooManyRules = errors.New(
		"project has too many automation rules",
	)

	ErrForbidden = errors.New("forbidden")
)
