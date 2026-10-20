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
)
