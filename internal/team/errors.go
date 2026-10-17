package team

import "errors"

var (
	ErrTeamNotFound = errors.New(
		"team not found",
	)

	ErrTeamNameRequired = errors.New(
		"team name is required",
	)

	ErrTeamNameTooLong = errors.New(
		"team name is too long",
	)

	ErrTeamDescriptionTooLong = errors.New(
		"team description is too long",
	)

	// ErrTeamMembersInvalid reports a user id that is not a workspace
	// member (or a duplicate in the request).
	ErrTeamMembersInvalid = errors.New(
		"team members invalid",
	)

	ErrNothingToUpdate = errors.New(
		"no changes requested",
	)

	ErrForbidden = errors.New(
		"forbidden",
	)

	// ErrTeamConflict is the unique (tenant, name) violation.
	ErrTeamConflict = errors.New(
		"team name already exists",
	)
)
