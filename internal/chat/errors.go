package chat

import "errors"

var (
	ErrInvalidID = errors.New(
		"invalid chat id",
	)

	ErrConversationNotFound = errors.New(
		"conversation not found",
	)

	ErrMemberNotFound = errors.New(
		"conversation member not found",
	)

	ErrMessageNotFound = errors.New(
		"chat message not found",
	)

	ErrForbidden = errors.New(
		"forbidden",
	)

	ErrInvalidConversationType = errors.New(
		"invalid conversation type",
	)

	ErrInvalidTitle = errors.New(
		"invalid conversation title",
	)

	ErrInvalidMembers = errors.New(
		"invalid conversation members",
	)

	ErrTooManyMembers = errors.New(
		"too many conversation members",
	)

	ErrCannotModifyMembers = errors.New(
		"conversation members cannot be modified",
	)

	ErrNotConversationOwner = errors.New(
		"conversation owner required",
	)

	ErrCannotRemoveSelf = errors.New(
		"cannot remove yourself from the conversation",
	)

	ErrInvalidReadCursor = errors.New(
		"invalid read cursor",
	)

	ErrInvalidCursor = errors.New(
		"invalid message cursor",
	)

	ErrProjectNotFound = errors.New(
		"project not found",
	)
)
