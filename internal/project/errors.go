package project

import "errors"

var (
	ErrNotFound      = errors.New("project not found")
	ErrInvalidID     = errors.New("invalid project id")
	ErrNameRequired  = errors.New("project name is required")
	ErrNameTooLong   = errors.New("project name is too long")
	ErrInvalidCursor = errors.New("invalid cursor")
	ErrInvalidName   = errors.New("invalid project name")
	ErrNoChanges     = errors.New("no changes provided")
	ErrForbidden     = errors.New("forbidden")
)
