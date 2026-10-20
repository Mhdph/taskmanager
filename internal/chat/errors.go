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

	ErrInvalidBody = errors.New(
		"invalid message body",
	)

	ErrInvalidClientMessageID = errors.New(
		"invalid client message id",
	)

	ErrInvalidAttachment = errors.New(
		"invalid attachment",
	)

	ErrInvalidAttachmentFilename = errors.New(
		"invalid attachment filename",
	)

	ErrAttachmentTooLarge = errors.New(
		"attachment too large",
	)

	ErrTooManyAttachments = errors.New(
		"too many attachments",
	)

	ErrAttachmentNotFound = errors.New(
		"attachment not found",
	)

	ErrAttachmentAttached = errors.New(
		"attachment already attached to a message",
	)

	ErrInvalidStreamToken = errors.New(
		"invalid or expired stream token",
	)

	ErrInvalidCursor = errors.New(
		"invalid message cursor",
	)

	ErrInvalidNotificationMode = errors.New(
		"invalid notification mode",
	)

	ErrProjectNotFound = errors.New(
		"project not found",
	)
)
