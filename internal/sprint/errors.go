package sprint

import "errors"

var (
	ErrInvalidID = errors.New(
		"invalid sprint id",
	)

	ErrSprintNotFound = errors.New(
		"sprint not found",
	)

	ErrInvalidName = errors.New(
		"invalid sprint name",
	)

	ErrInvalidDateRange = errors.New(
		"invalid sprint date range",
	)

	ErrInvalidGoal = errors.New(
		"invalid sprint goal",
	)

	ErrInvalidState = errors.New(
		"invalid sprint state",
	)

	// ErrLifecycleConflict is returned when the requested transition is
	// not possible from the sprint's current state.
	ErrLifecycleConflict = errors.New(
		"sprint state does not allow this operation",
	)

	// ErrActiveSprintExists is returned when starting a sprint would give
	// the project a second active sprint.
	ErrActiveSprintExists = errors.New(
		"project already has an active sprint",
	)

	ErrTaskNotFound = errors.New(
		"task not found",
	)

	// ErrTaskNotEligible is returned when a task cannot change its sprint
	// assignment, for example because it is archived or already finished.
	ErrTaskNotEligible = errors.New(
		"task cannot be assigned to a sprint",
	)

	// ErrCarryoverTargetInvalid is returned when the carryover
	// destination is not a planned sprint of the same project.
	ErrCarryoverTargetInvalid = errors.New(
		"invalid carryover sprint",
	)

	ErrInvalidCursor = errors.New(
		"invalid sprint cursor",
	)

	ErrForbidden = errors.New(
		"forbidden",
	)
)
