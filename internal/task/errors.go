package task

import "errors"

var (
	ErrInvalidID       = errors.New("invalid task id")
	ErrInvalidTitle    = errors.New("invalid task title")
	ErrInvalidPriority = errors.New("invalid task priority")
	ErrInvalidDueDate  = errors.New("invalid due date")

	ErrTaskNotFound = errors.New("task not found")

	ErrStatusNotFound = errors.New("task status not found")

	ErrAssigneeNotMember = errors.New(
		"assignee is not a project member",
	)

	ErrParentTaskNotFound = errors.New(
		"parent task not found",
	)

	ErrInvalidParentTask = errors.New(
		"invalid parent task",
	)
	ErrNoChanges = errors.New(
		"there is no changes in task",
	)
)
