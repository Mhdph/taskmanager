package member

import "errors"

var (
	ErrForbidden = errors.New("forbidden")
	ErrNotFound  = errors.New("member not found")

	ErrInvalidRole   = errors.New("invalid member role")
	ErrInvalidStatus = errors.New("invalid member status")

	ErrLastOwner = errors.New("workspace must have at least one owner")
)
