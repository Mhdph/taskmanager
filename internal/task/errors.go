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
		"no changes provided",
	)

	ErrDependencyExists = errors.New(
		"task dependency already exists",
	)

	ErrDependencyNotFound = errors.New(
		"task dependency not found",
	)

	ErrDependencyCycle = errors.New(
		"task dependency would create a cycle",
	)

	ErrSelfDependency = errors.New(
		"task cannot depend on itself",
	)

	ErrWatcherExists = errors.New(
		"task watcher already exists",
	)

	ErrWatcherNotFound = errors.New(
		"task watcher not found",
	)

	ErrWatcherNotMember = errors.New(
		"watcher is not a project member",
	)
)
