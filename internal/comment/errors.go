package comment

import "errors"

var (
	ErrInvalidID = errors.New(
		"invalid comment id",
	)

	ErrInvalidBody = errors.New(
		"invalid comment body",
	)

	ErrCommentNotFound = errors.New(
		"comment not found",
	)

	ErrMentionedUserNotMember = errors.New(
		"mentioned user is not a project member",
	)

	ErrForbidden = errors.New(
		"forbidden",
	)
)
