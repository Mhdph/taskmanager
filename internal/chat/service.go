package chat

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/storage"
)

const (
	maxGroupTitleLength = 200

	maxGroupMembers = 50

	defaultMessagePageSize = 50
	maxMessagePageSize     = 100

	maxConversationPageSize = 100

	maxMessageBodyLength = 10000

	maxClientMessageIDLength = 128

	maxAttachmentsPerMessage = 10

	maxAttachmentFilenameLength = 500

	maxAttachmentContentTypeLength = 255

	attachmentDownloadExpiry = 5 * time.Minute

	attachmentCleanupBatch = 100

	maxNotificationPreviewRunes = 200

	streamTokenTTL = 60 * time.Second

	streamTokenBytes = 32

	maxStreamTokenLength = 128
)

// FileStorage is the object-storage subset used by chat attachments.
type FileStorage interface {
	Upload(
		ctx context.Context,
		input storage.UploadInput,
	) error

	Delete(
		ctx context.Context,
		objectKey string,
	) error

	PresignedDownloadURL(
		ctx context.Context,
		objectKey string,
		filename string,
		expiry time.Duration,
	) (string, error)
}

// Publisher is the broker subset used for events that are not tied to a
// write transaction (read receipts).
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// PresenceChecker reports which users have a live stream connection on any
// API instance. Offline chat notifications use it to avoid notifying a user
// who already received the message over WebSocket.
type PresenceChecker interface {
	OnlineUserIDs(
		ctx context.Context,
		tenantID string,
		userIDs []string,
	) ([]string, error)
}

// NotificationSender enqueues an offline notification (the production
// implementation is notification.Dispatcher).
type NotificationSender interface {
	Send(ctx context.Context, input notification.SendInput) error
}

// Service owns conversation access rules. Project conversations delegate to
// the project authorizer so chat visibility can never drift from project
// visibility; DM and group conversations are member-only.
type Service struct {
	repository *Repository

	projectRepository *project.Repository

	projectAuthorizer *project.Authorizer

	publisher Publisher

	fileStorage FileStorage

	maxUploadSize int64

	presence PresenceChecker

	notifications NotificationSender
}

func NewService(
	repository *Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
	publisher Publisher,
	fileStorage FileStorage,
	maxUploadSize int64,
	presence PresenceChecker,
	notifications NotificationSender,
) *Service {
	return &Service{
		repository: repository,

		projectRepository: projectRepository,

		projectAuthorizer: projectAuthorizer,

		publisher: publisher,

		fileStorage: fileStorage,

		maxUploadSize: maxUploadSize,

		presence: presence,

		notifications: notifications,
	}
}

type CreateGroupInput struct {
	Identity auth.Identity

	Title string

	// MemberIDs excludes the creator, who is always added as the owner.
	MemberIDs []string
}

func (s *Service) CreateGroup(
	ctx context.Context,
	input CreateGroupInput,
) (ConversationDetail, error) {
	title := strings.TrimSpace(input.Title)

	if title == "" || utf8.RuneCountInString(title) > maxGroupTitleLength {
		return ConversationDetail{}, ErrInvalidTitle
	}

	members := dedupeUserIDs(input.MemberIDs)

	members = removeUserID(members, input.Identity.UserID)

	if len(members) > maxGroupMembers {
		return ConversationDetail{}, ErrTooManyMembers
	}

	if err := s.requireWorkspaceMembers(ctx, input.Identity.TenantID, members); err != nil {
		return ConversationDetail{}, err
	}

	conversation, err := s.repository.CreateGroupConversation(
		ctx,
		CreateGroupConversationParams{
			ID:              uuid.NewString(),
			TenantID:        input.Identity.TenantID,
			Title:           title,
			CreatedByUserID: input.Identity.UserID,
			MemberIDs:       members,
		},
	)
	if err != nil {
		return ConversationDetail{}, err
	}

	return s.detail(ctx, input.Identity.TenantID, conversation.ID)
}

type CreateDMInput struct {
	Identity auth.Identity

	OtherUserID string
}

func (s *Service) CreateDM(
	ctx context.Context,
	input CreateDMInput,
) (ConversationDetail, error) {
	otherUserID := strings.TrimSpace(input.OtherUserID)

	if otherUserID == "" || otherUserID == input.Identity.UserID {
		return ConversationDetail{}, ErrInvalidMembers
	}

	if err := s.requireWorkspaceMembers(
		ctx,
		input.Identity.TenantID,
		[]string{otherUserID},
	); err != nil {
		return ConversationDetail{}, err
	}

	conversation, err := s.repository.CreateDMConversation(
		ctx,
		input.Identity.TenantID,
		input.Identity.UserID,
		otherUserID,
		input.Identity.UserID,
		uuid.NewString(),
	)
	if err != nil {
		return ConversationDetail{}, err
	}

	return s.detail(ctx, input.Identity.TenantID, conversation.ID)
}

func (s *Service) List(
	ctx context.Context,
	identity auth.Identity,
) ([]ConversationSummary, error) {
	return s.repository.ListForUser(
		ctx,
		identity.TenantID,
		identity.UserID,
		identity.IsAdmin(),
		maxConversationPageSize,
	)
}

func (s *Service) Get(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
) (ConversationDetail, error) {
	conversation, _, err := s.authorize(ctx, identity, conversationID)
	if err != nil {
		return ConversationDetail{}, err
	}

	return s.detail(ctx, identity.TenantID, conversation.ID)
}

// OnlineMemberIDs returns the conversation members with a live stream
// connection on any API instance, preserving member order.
func (s *Service) OnlineMemberIDs(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
) ([]string, error) {
	detail, err := s.Get(ctx, identity, conversationID)
	if err != nil {
		return nil, err
	}

	userIDs := make([]string, 0, len(detail.Members))
	for _, member := range detail.Members {
		userIDs = append(userIDs, member.UserID)
	}

	online, err := s.repository.OnlineUserIDs(
		ctx,
		identity.TenantID,
		userIDs,
	)
	if err != nil {
		return nil, err
	}

	onlineSet := make(map[string]struct{}, len(online))
	for _, userID := range online {
		onlineSet[userID] = struct{}{}
	}

	result := make([]string, 0, len(online))

	for _, member := range detail.Members {
		if _, ok := onlineSet[member.UserID]; ok {
			result = append(result, member.UserID)
		}
	}

	return result, nil
}

// GetNotificationPreference returns the caller's notification mode for a
// conversation. Members without an explicit preference get "all".
func (s *Service) GetNotificationPreference(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
) (NotificationMode, error) {
	conversation, _, err := s.authorize(
		ctx,
		identity,
		conversationID,
	)
	if err != nil {
		return "", err
	}

	return s.repository.GetNotificationPreference(
		ctx,
		identity.TenantID,
		conversation.ID,
		identity.UserID,
	)
}

// SetNotificationPreference stores the caller's notification mode for a
// conversation. Muting only silences push notifications; the stream still
// delivers every message.
func (s *Service) SetNotificationPreference(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
	mode NotificationMode,
) (NotificationMode, error) {
	if !mode.valid() {
		return "", ErrInvalidNotificationMode
	}

	conversation, _, err := s.authorize(
		ctx,
		identity,
		conversationID,
	)
	if err != nil {
		return "", err
	}

	if err := s.repository.SetNotificationPreference(
		ctx,
		identity.TenantID,
		conversation.ID,
		identity.UserID,
		mode,
	); err != nil {
		return "", err
	}

	return mode, nil
}

// ProjectChat returns the project's conversation, creating it on first
// access and refreshing its membership from the project.
func (s *Service) ProjectChat(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (ConversationDetail, error) {
	_, err := s.projectRepository.GetForTenant(
		ctx,
		identity.TenantID,
		projectID,
	)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return ConversationDetail{}, ErrProjectNotFound
		}

		if errors.Is(err, project.ErrInvalidID) {
			return ConversationDetail{}, ErrInvalidID
		}

		return ConversationDetail{}, fmt.Errorf("get project: %w", err)
	}

	if err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewProject,
	); err != nil {
		return ConversationDetail{}, normalizeAuthorizerError(err)
	}

	conversation, err := s.repository.CreateProjectConversation(
		ctx,
		CreateProjectConversationParams{
			ID:              uuid.NewString(),
			TenantID:        identity.TenantID,
			ProjectID:       projectID,
			CreatedByUserID: identity.UserID,
		},
	)
	if err != nil {
		return ConversationDetail{}, err
	}

	if err := s.repository.SyncProjectMembers(
		ctx,
		identity.TenantID,
		conversation.ID,
		projectID,
	); err != nil {
		return ConversationDetail{}, err
	}

	// Workspace admins are not project members; keep an explicit row so
	// they have a read cursor and appear in their conversation list.
	if _, err := s.repository.GetMember(
		ctx,
		identity.TenantID,
		conversation.ID,
		identity.UserID,
	); errors.Is(err, ErrMemberNotFound) {
		if err := s.repository.AddMember(
			ctx,
			identity.TenantID,
			conversation.ID,
			identity.UserID,
			MemberRoleMember,
		); err != nil {
			return ConversationDetail{}, err
		}
	} else if err != nil {
		return ConversationDetail{}, err
	}

	return s.detail(ctx, identity.TenantID, conversation.ID)
}

type ListMessagesInput struct {
	Identity auth.Identity

	ConversationID string

	// Before is an opaque cursor from a previous history page.
	Before string

	// After is a raw sequence number for forward replay after a known
	// message. Before and After are mutually exclusive.
	After string

	Limit int
}

func (s *Service) ListMessages(
	ctx context.Context,
	input ListMessagesInput,
) (MessagePage, error) {
	conversation, _, err := s.authorize(
		ctx,
		input.Identity,
		input.ConversationID,
	)
	if err != nil {
		return MessagePage{}, err
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultMessagePageSize
	}
	if limit > maxMessagePageSize {
		limit = maxMessagePageSize
	}

	if input.Before != "" && input.After != "" {
		return MessagePage{}, ErrInvalidCursor
	}

	// Forward replay: everything newer than a known sequence, oldest first.
	if input.After != "" {
		afterSeq, err := parseAfterSequence(input.After)
		if err != nil {
			return MessagePage{}, err
		}

		items, err := s.repository.ListMessagesAfter(
			ctx,
			input.Identity.TenantID,
			conversation.ID,
			afterSeq,
			int32(limit+1),
		)
		if err != nil {
			return MessagePage{}, err
		}

		hasMore := len(items) > limit
		if hasMore {
			items = items[:limit]
		}

		var nextAfter *int64
		if hasMore && len(items) > 0 {
			value := items[len(items)-1].Seq
			nextAfter = &value
		}

		return MessagePage{
			Items:     items,
			NextAfter: nextAfter,
		}, nil
	}

	var beforeSeq *int64
	if input.Before != "" {
		seq, err := DecodeMessageCursor(input.Before)
		if err != nil {
			return MessagePage{}, err
		}

		beforeSeq = &seq
	}

	// Fetch one extra row to detect whether an older page exists.
	items, err := s.repository.ListMessages(
		ctx,
		input.Identity.TenantID,
		conversation.ID,
		beforeSeq,
		int32(limit+1),
	)
	if err != nil {
		return MessagePage{}, err
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		value := EncodeMessageCursor(items[len(items)-1].Seq)
		nextCursor = &value
	}

	// The repository returns newest first; chat renders oldest first.
	reverseMessages(items)

	return MessagePage{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func parseAfterSequence(value string) (int64, error) {
	seq, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || seq < 0 {
		return 0, ErrInvalidCursor
	}

	return seq, nil
}

type SendMessageInput struct {
	Identity auth.Identity

	ConversationID string

	Body string

	// ClientMessageID is optional; when present it makes retries
	// idempotent.
	ClientMessageID *string

	// AttachmentIDs references pending uploads from UploadAttachment.
	AttachmentIDs []string
}

// SendMessage persists a user message. The bool reports whether a new row
// was created; false means the client message id was already stored and the
// original message is returned.
func (s *Service) SendMessage(
	ctx context.Context,
	input SendMessageInput,
) (Message, bool, error) {
	body, err := normalizeOptionalMessageBody(input.Body)
	if err != nil {
		return Message{}, false, err
	}

	attachmentIDs, err := normalizeAttachmentIDs(
		input.AttachmentIDs,
	)
	if err != nil {
		return Message{}, false, err
	}

	// Chat allows attachment-only messages, but not empty messages.
	if body == "" && len(attachmentIDs) == 0 {
		return Message{}, false, ErrInvalidBody
	}

	clientMessageID, err := normalizeClientMessageID(
		input.ClientMessageID,
	)
	if err != nil {
		return Message{}, false, err
	}

	conversation, _, err := s.authorize(
		ctx,
		input.Identity,
		input.ConversationID,
	)
	if err != nil {
		return Message{}, false, err
	}

	authorUserID := input.Identity.UserID

	message, created, err := s.repository.CreateMessage(
		ctx,
		CreateMessageParams{
			ID: uuid.NewString(),

			TenantID: input.Identity.TenantID,

			ConversationID: conversation.ID,

			Kind: MessageKindUser,

			AuthorUserID: &authorUserID,

			Body: body,

			ClientMessageID: clientMessageID,

			AttachmentIDs: attachmentIDs,
		},
	)
	if err != nil {
		return Message{}, false, err
	}

	// Idempotent retries must not notify twice. Delivery is best-effort:
	// a notification failure never fails the message send.
	if created {
		s.notifyMessageCreated(conversation, message)
	}

	return message, created, nil
}

func (s *Service) notifyMessageCreated(
	conversation Conversation,
	message Message,
) {
	if s.notifications == nil {
		return
	}

	// The message is committed. A detached context keeps notification
	// enqueueing from being skipped when the sender disconnects
	// immediately after the write.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	members, err := s.repository.ListMembers(
		ctx,
		message.TenantID,
		conversation.ID,
	)
	if err != nil {
		log.Printf(
			"list members for chat notification: %v",
			err,
		)
		return
	}

	authorUserID := ""
	if message.AuthorUserID != nil {
		authorUserID = *message.AuthorUserID
	}

	title := s.chatNotificationTitle(ctx, conversation)

	body := chatNotificationBody(message)

	conversationID := conversation.ID
	messageID := message.ID

	candidates := make([]string, 0, len(members))
	for _, member := range members {
		if member.UserID != authorUserID {
			candidates = append(candidates, member.UserID)
		}
	}

	// Per-conversation preferences decide who is notified at all. A member
	// without a stored preference uses the default mode.
	preferences, err := s.repository.ListNotificationPreferences(
		ctx,
		message.TenantID,
		conversation.ID,
		candidates,
	)
	if err != nil {
		// Fail open: notifying is better than silently dropping a
		// notification because preferences could not be read.
		log.Printf("list chat notification preferences: %v", err)
	}

	mentioned := make(map[string]struct{}, len(message.Mentions))
	for _, userID := range message.Mentions {
		mentioned[userID] = struct{}{}
	}

	recipients := make([]string, 0, len(candidates))

	for _, userID := range candidates {
		mode := preferences[userID]
		if mode == "" {
			mode = NotificationModeAll
		}

		switch mode {
		case NotificationModeMute:
			continue

		case NotificationModeMentions:
			if _, ok := mentioned[userID]; !ok {
				continue
			}
		}

		recipients = append(recipients, userID)
	}

	// Presence is shared across instances, so a user connected anywhere is
	// skipped. One batched query keeps notification fan-out independent of
	// conversation size.
	online := map[string]struct{}{}

	if s.presence != nil && len(recipients) > 0 {
		userIDs, err := s.presence.OnlineUserIDs(
			ctx,
			message.TenantID,
			recipients,
		)
		if err != nil {
			// Fail open: a duplicate notification is better than silently
			// dropping one because the presence query failed.
			log.Printf("check chat notification presence: %v", err)
		} else {
			for _, userID := range userIDs {
				online[userID] = struct{}{}
			}
		}
	}

	for _, userID := range recipients {
		// Online users get the message over the stream; a notification
		// would duplicate it.
		if _, ok := online[userID]; ok {
			continue
		}

		input := notification.SendInput{
			TenantID: message.TenantID,

			RecipientUserID: userID,

			Type: notification.EventChatMessage,

			ActorUserID: message.AuthorUserID,

			ProjectID: conversation.ProjectID,

			ConversationID: &conversationID,

			MessageID: &messageID,

			Title: title,

			Body: &body,

			Data: map[string]any{
				"conversationId": conversationID,

				"conversationType": string(conversation.Type),

				"messageId": messageID,

				"seq": message.Seq,

				"attachmentCount": len(message.Attachments),
			},
		}

		if err := s.notifications.Send(ctx, input); err != nil {
			log.Printf(
				"send chat notification to %s: %v",
				userID,
				err,
			)

			if errors.Is(err, notification.ErrQueueFull) {
				return
			}
		}
	}
}

func (s *Service) chatNotificationTitle(
	ctx context.Context,
	conversation Conversation,
) string {
	switch conversation.Type {
	case ConversationTypeDM:
		return "New direct message"

	case ConversationTypeGroup:
		if conversation.Title != "" {
			return "New message in " + conversation.Title
		}

		return "New group message"

	case ConversationTypeProject:
		if conversation.ProjectID != nil && s.projectRepository != nil {
			projectItem, err := s.projectRepository.GetForTenant(
				ctx,
				conversation.TenantID,
				*conversation.ProjectID,
			)
			if err == nil && projectItem.Name != "" {
				return "New message in " + projectItem.Name
			}
		}

		return "New message in project chat"

	default:
		return "New chat message"
	}
}

func chatNotificationBody(message Message) string {
	preview := strings.Join(
		strings.Fields(message.Body),
		" ",
	)

	switch attachments := len(message.Attachments); {
	case attachments == 1 && preview == "":
		preview = "Sent an attachment"

	case attachments > 1 && preview == "":
		preview = fmt.Sprintf("Sent %d attachments", attachments)

	case attachments == 1:
		preview += " (1 attachment)"

	case attachments > 1:
		preview += fmt.Sprintf(" (%d attachments)", attachments)
	}

	if preview == "" {
		preview = "New message"
	}

	if utf8.RuneCountInString(preview) > maxNotificationPreviewRunes {
		preview = string(
			[]rune(preview)[:maxNotificationPreviewRunes],
		) + "…"
	}

	return preview
}

type TypingInput struct {
	Identity auth.Identity

	ConversationID string

	Typing bool
}

// SetTyping validates membership and publishes an ephemeral typing event.
// It is never persisted; clients that are offline simply miss the indicator.
func (s *Service) SetTyping(
	ctx context.Context,
	input TypingInput,
) error {
	conversation, _, err := s.authorize(
		ctx,
		input.Identity,
		input.ConversationID,
	)
	if err != nil {
		return err
	}

	if s.publisher == nil {
		return nil
	}

	return s.publisher.Publish(ctx, Event{
		Kind:           EventTyping,
		TenantID:       input.Identity.TenantID,
		ConversationID: conversation.ID,
		ActorUserID:    input.Identity.UserID,
		Typing:         input.Typing,
	})
}

type EditMessageInput struct {
	Identity auth.Identity

	ConversationID string
	MessageID      string

	Body string
}

func (s *Service) EditMessage(
	ctx context.Context,
	input EditMessageInput,
) (Message, error) {
	body, err := normalizeMessageBody(input.Body)
	if err != nil {
		return Message{}, err
	}

	conversation, _, err := s.authorize(
		ctx,
		input.Identity,
		input.ConversationID,
	)
	if err != nil {
		return Message{}, err
	}

	message, err := s.repository.GetMessage(
		ctx,
		input.Identity.TenantID,
		conversation.ID,
		input.MessageID,
	)
	if err != nil {
		return Message{}, err
	}

	if message.AuthorUserID == nil ||
		*message.AuthorUserID != input.Identity.UserID {

		return Message{}, ErrForbidden
	}

	return s.repository.EditMessage(
		ctx,
		input.Identity.TenantID,
		conversation.ID,
		message.ID,
		body,
	)
}

type DeleteMessageInput struct {
	Identity auth.Identity

	ConversationID string
	MessageID      string
}

// DeleteMessage tombstones a message. Authors can delete their own
// messages; conversation owners (project managers) can moderate any message.
func (s *Service) DeleteMessage(
	ctx context.Context,
	input DeleteMessageInput,
) (Message, error) {
	conversation, member, err := s.authorize(
		ctx,
		input.Identity,
		input.ConversationID,
	)
	if err != nil {
		return Message{}, err
	}

	message, err := s.repository.GetMessage(
		ctx,
		input.Identity.TenantID,
		conversation.ID,
		input.MessageID,
	)
	if err != nil {
		return Message{}, err
	}

	isAuthor := message.AuthorUserID != nil &&
		*message.AuthorUserID == input.Identity.UserID

	if !isAuthor && member.Role != MemberRoleOwner {
		return Message{}, ErrForbidden
	}

	deleted, attachments, err := s.repository.DeleteMessage(
		ctx,
		input.Identity.TenantID,
		conversation.ID,
		message.ID,
	)
	if err != nil {
		return Message{}, err
	}

	// The metadata is already committed; failing to remove an object must
	// not fail the request, but it is logged for storage cleanup.
	s.deleteAttachmentObjects(ctx, attachments)

	return deleted, nil
}

type UploadAttachmentInput struct {
	Identity auth.Identity

	ConversationID string

	Filename string

	ContentType string

	Size int64

	Reader io.Reader
}

// UploadRequestLimit is the multipart body cap: the file limit plus room for
// form overhead.
func (s *Service) UploadRequestLimit() int64 {
	if s.maxUploadSize <= 0 {
		return 32 << 20
	}

	return s.maxUploadSize + (1 << 20)
}

// UploadAttachment stores the object first and then the pending metadata.
// The row is not visible in any message until SendMessage binds it.
func (s *Service) UploadAttachment(
	ctx context.Context,
	input UploadAttachmentInput,
) (Attachment, error) {
	conversation, _, err := s.authorize(
		ctx,
		input.Identity,
		input.ConversationID,
	)
	if err != nil {
		return Attachment{}, err
	}

	filename, err := sanitizeAttachmentFilename(input.Filename)
	if err != nil {
		return Attachment{}, err
	}

	if input.Size <= 0 {
		return Attachment{}, ErrInvalidAttachment
	}

	if s.maxUploadSize > 0 && input.Size > s.maxUploadSize {
		return Attachment{}, ErrAttachmentTooLarge
	}

	if s.fileStorage == nil {
		return Attachment{}, storage.ErrStorageDisabled
	}

	attachmentID := uuid.NewString()

	objectKey := fmt.Sprintf(
		"tenants/%s/chat/%s/%s",
		input.Identity.TenantID,
		conversation.ID,
		attachmentID,
	)

	if err := s.fileStorage.Upload(ctx, storage.UploadInput{
		ObjectKey: objectKey,

		Reader: input.Reader,

		Size: input.Size,

		ContentType: normalizeAttachmentContentType(
			input.ContentType,
		),
	}); err != nil {
		return Attachment{}, fmt.Errorf(
			"upload attachment object: %w",
			err,
		)
	}

	attachment, err := s.repository.CreateAttachment(
		ctx,
		CreateAttachmentParams{
			ID: attachmentID,

			TenantID: input.Identity.TenantID,

			ConversationID: conversation.ID,

			UploadedByUserID: input.Identity.UserID,

			Filename: filename,

			ContentType: normalizeAttachmentContentType(
				input.ContentType,
			),

			SizeBytes: input.Size,

			ObjectKey: objectKey,
		},
	)
	if err != nil {
		if cleanupErr := s.fileStorage.Delete(
			ctx,
			objectKey,
		); cleanupErr != nil {
			log.Printf(
				"cleanup orphan chat attachment %s: %v",
				objectKey,
				cleanupErr,
			)
		}

		return Attachment{}, fmt.Errorf(
			"save attachment metadata: %w",
			err,
		)
	}

	return attachment, nil
}

// CancelAttachment removes an upload that has not been sent. Once bound to
// a message, only message deletion removes it.
func (s *Service) CancelAttachment(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
	attachmentID string,
) error {
	conversation, _, err := s.authorize(
		ctx,
		identity,
		conversationID,
	)
	if err != nil {
		return err
	}

	attachment, err := s.repository.GetAttachment(
		ctx,
		identity.TenantID,
		conversation.ID,
		attachmentID,
	)
	if err != nil {
		return err
	}

	if attachment.UploadedByUserID != identity.UserID {
		return ErrForbidden
	}

	if attachment.MessageID != nil {
		return ErrAttachmentAttached
	}

	// Delete the row first: a concurrent send that attaches it either wins
	// (and this returns a conflict) or loses (and its transaction rolls
	// back). Object cleanup is best-effort afterwards.
	removed, err := s.repository.DeletePendingAttachment(
		ctx,
		identity.TenantID,
		conversation.ID,
		attachment.ID,
		identity.UserID,
	)
	if err != nil {
		return err
	}

	if !removed {
		return ErrAttachmentAttached
	}

	if s.fileStorage != nil {
		cleanupCtx, cleanupCancel := attachmentStorageContext()
		defer cleanupCancel()

		if err := s.fileStorage.Delete(
			cleanupCtx,
			attachment.ObjectKey,
		); err != nil && !errors.Is(
			err,
			storage.ErrStorageDisabled,
		) {
			log.Printf(
				"delete chat attachment object %s: %v",
				attachment.ID,
				err,
			)
		}
	}

	return nil
}

type AttachmentDownload struct {
	URL string

	ExpiresAt time.Time
}

func (s *Service) GetAttachmentDownload(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
	attachmentID string,
) (AttachmentDownload, error) {
	conversation, _, err := s.authorize(
		ctx,
		identity,
		conversationID,
	)
	if err != nil {
		return AttachmentDownload{}, err
	}

	attachment, err := s.repository.GetAttachment(
		ctx,
		identity.TenantID,
		conversation.ID,
		attachmentID,
	)
	if err != nil {
		return AttachmentDownload{}, err
	}

	if attachment.MessageID == nil {
		return AttachmentDownload{}, ErrAttachmentNotFound
	}

	message, err := s.repository.GetMessage(
		ctx,
		identity.TenantID,
		conversation.ID,
		*attachment.MessageID,
	)
	if err != nil || message.DeletedAt != nil {
		return AttachmentDownload{}, ErrAttachmentNotFound
	}

	if s.fileStorage == nil {
		return AttachmentDownload{}, storage.ErrStorageDisabled
	}

	downloadURL, err := s.fileStorage.PresignedDownloadURL(
		ctx,
		attachment.ObjectKey,
		attachment.Filename,
		attachmentDownloadExpiry,
	)
	if err != nil {
		return AttachmentDownload{}, fmt.Errorf(
			"create attachment download url: %w",
			err,
		)
	}

	return AttachmentDownload{
		URL: downloadURL,

		ExpiresAt: time.Now().UTC().Add(
			attachmentDownloadExpiry,
		),
	}, nil
}

// RunAttachmentCleanup sweeps uploads that were never sent. It runs once at
// startup and then on every interval until the context is canceled.
func (s *Service) RunAttachmentCleanup(
	ctx context.Context,
	interval time.Duration,
	maxAge time.Duration,
) {
	if s.fileStorage == nil {
		return
	}

	s.cleanupStaleAttachments(ctx, maxAge)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			s.cleanupStaleAttachments(ctx, maxAge)
		}
	}
}

// CleanupStaleAttachments deletes pending uploads older than maxAge. It is
// exported for the startup worker and tests.
func (s *Service) CleanupStaleAttachments(
	ctx context.Context,
	maxAge time.Duration,
) (int, error) {
	if s.fileStorage == nil {
		return 0, nil
	}

	items, err := s.repository.ListStalePendingAttachments(
		ctx,
		time.Now().UTC().Add(-maxAge),
		attachmentCleanupBatch,
	)
	if err != nil {
		return 0, err
	}

	removed := 0

	for _, attachment := range items {
		ok, err := s.repository.DeletePendingAttachment(
			ctx,
			attachment.TenantID,
			attachment.ConversationID,
			attachment.ID,
			attachment.UploadedByUserID,
		)
		if err != nil {
			log.Printf(
				"cleanup chat attachment %s: %v",
				attachment.ID,
				err,
			)
			continue
		}

		if !ok {
			continue
		}

		if err := s.fileStorage.Delete(
			ctx,
			attachment.ObjectKey,
		); err != nil && !errors.Is(
			err,
			storage.ErrStorageDisabled,
		) {
			log.Printf(
				"cleanup chat attachment object %s: %v",
				attachment.ID,
				err,
			)
		}

		removed++
	}

	return removed, nil
}

func (s *Service) cleanupStaleAttachments(
	ctx context.Context,
	maxAge time.Duration,
) {
	removed, err := s.CleanupStaleAttachments(ctx, maxAge)
	if err != nil {
		log.Printf("cleanup stale chat attachments: %v", err)
		return
	}

	if removed > 0 {
		log.Printf("removed %d stale chat attachments", removed)
	}
}

func attachmentStorageContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *Service) deleteAttachmentObjects(
	ctx context.Context,
	attachments []Attachment,
) {
	if s.fileStorage == nil {
		return
	}

	// The metadata is already committed. Use a detached context so a
	// client disconnect cannot leave the object behind.
	cleanupCtx, cleanupCancel := attachmentStorageContext()
	defer cleanupCancel()

	for _, attachment := range attachments {
		if err := s.fileStorage.Delete(
			cleanupCtx,
			attachment.ObjectKey,
		); err != nil && !errors.Is(
			err,
			storage.ErrStorageDisabled,
		) {
			log.Printf(
				"delete chat attachment object %s: %v",
				attachment.ID,
				err,
			)
		}
	}
}

func (s *Service) MarkRead(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
	lastReadSeq int64,
) error {
	if lastReadSeq < 0 {
		return ErrInvalidReadCursor
	}

	conversation, _, err := s.authorize(ctx, identity, conversationID)
	if err != nil {
		return err
	}

	updated, err := s.repository.UpdateReadCursor(
		ctx,
		identity.TenantID,
		conversation.ID,
		identity.UserID,
		lastReadSeq,
	)
	if err != nil {
		return err
	}

	if !updated {
		return ErrForbidden
	}

	// Read receipts are ephemeral for the writer: the cursor is already
	// saved, so a failed publish must not fail the request.
	if s.publisher != nil {
		err := s.publisher.Publish(
			ctx,
			Event{
				Kind:           EventConversationRead,
				TenantID:       identity.TenantID,
				ConversationID: conversation.ID,
				ActorUserID:    identity.UserID,
				LastReadSeq:    lastReadSeq,
			},
		)
		if err != nil {
			log.Printf("publish read receipt: %v", err)
		}
	}

	return nil
}

func (s *Service) AddMember(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
	userID string,
) error {
	conversation, member, err := s.authorize(
		ctx,
		identity,
		conversationID,
	)
	if err != nil {
		return err
	}

	if conversation.Type != ConversationTypeGroup {
		return ErrCannotModifyMembers
	}

	if member.Role != MemberRoleOwner {
		return ErrNotConversationOwner
	}

	target := strings.TrimSpace(userID)
	if target == "" {
		return ErrInvalidMembers
	}

	if err := s.requireWorkspaceMembers(
		ctx,
		identity.TenantID,
		[]string{target},
	); err != nil {
		return err
	}

	return s.repository.AddMember(
		ctx,
		identity.TenantID,
		conversation.ID,
		target,
		MemberRoleMember,
	)
}

func (s *Service) RemoveMember(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
	userID string,
) error {
	conversation, member, err := s.authorize(
		ctx,
		identity,
		conversationID,
	)
	if err != nil {
		return err
	}

	if conversation.Type != ConversationTypeGroup {
		return ErrCannotModifyMembers
	}

	if member.Role != MemberRoleOwner {
		return ErrNotConversationOwner
	}

	target := strings.TrimSpace(userID)
	if target == identity.UserID {
		return ErrCannotRemoveSelf
	}

	removed, err := s.repository.RemoveMember(
		ctx,
		identity.TenantID,
		conversation.ID,
		target,
	)
	if err != nil {
		return err
	}

	if !removed {
		return ErrMemberNotFound
	}

	return nil
}

// -------------------------------------------------------------------------
// Access
// -------------------------------------------------------------------------

// authorize resolves a conversation and proves the identity may read it,
// returning the caller's membership (read cursor included).
func (s *Service) authorize(
	ctx context.Context,
	identity auth.Identity,
	conversationID string,
) (Conversation, Member, error) {
	conversation, err := s.repository.GetConversation(
		ctx,
		identity.TenantID,
		conversationID,
	)
	if err != nil {
		return Conversation{}, Member{}, err
	}

	if conversation.Type != ConversationTypeProject {
		member, err := s.repository.GetMember(
			ctx,
			identity.TenantID,
			conversation.ID,
			identity.UserID,
		)
		if errors.Is(err, ErrMemberNotFound) {
			return Conversation{}, Member{}, ErrForbidden
		}
		if err != nil {
			return Conversation{}, Member{}, err
		}

		return conversation, member, nil
	}

	if conversation.ProjectID == nil {
		return Conversation{}, Member{}, ErrConversationNotFound
	}

	if err := s.projectAuthorizer.Require(
		ctx,
		identity,
		*conversation.ProjectID,
		project.PermissionViewProject,
	); err != nil {
		return Conversation{}, Member{}, normalizeAuthorizerError(err)
	}

	member, err := s.repository.GetMember(
		ctx,
		identity.TenantID,
		conversation.ID,
		identity.UserID,
	)
	if err == nil {
		return conversation, member, nil
	}
	if !errors.Is(err, ErrMemberNotFound) {
		return Conversation{}, Member{}, err
	}

	// The caller can read the project but has no conversation row yet:
	// either the membership sync has not run, or the caller is an admin
	// who is not on the project.
	if err := s.repository.SyncProjectMembers(
		ctx,
		identity.TenantID,
		conversation.ID,
		*conversation.ProjectID,
	); err != nil {
		return Conversation{}, Member{}, err
	}

	member, err = s.repository.GetMember(
		ctx,
		identity.TenantID,
		conversation.ID,
		identity.UserID,
	)
	if err == nil {
		return conversation, member, nil
	}
	if !errors.Is(err, ErrMemberNotFound) {
		return Conversation{}, Member{}, err
	}

	if !identity.IsAdmin() {
		return Conversation{}, Member{}, ErrForbidden
	}

	if err := s.repository.AddMember(
		ctx,
		identity.TenantID,
		conversation.ID,
		identity.UserID,
		MemberRoleMember,
	); err != nil {
		return Conversation{}, Member{}, err
	}

	member, err = s.repository.GetMember(
		ctx,
		identity.TenantID,
		conversation.ID,
		identity.UserID,
	)
	if err != nil {
		return Conversation{}, Member{}, err
	}

	return conversation, member, nil
}

func (s *Service) detail(
	ctx context.Context,
	tenantID string,
	conversationID string,
) (ConversationDetail, error) {
	conversation, err := s.repository.GetConversation(
		ctx,
		tenantID,
		conversationID,
	)
	if err != nil {
		return ConversationDetail{}, err
	}

	members, err := s.repository.ListMembers(ctx, tenantID, conversationID)
	if err != nil {
		return ConversationDetail{}, err
	}

	return ConversationDetail{
		Conversation: conversation,
		Members:      members,
	}, nil
}

func (s *Service) requireWorkspaceMembers(
	ctx context.Context,
	tenantID string,
	userIDs []string,
) error {
	if len(userIDs) == 0 {
		return nil
	}

	existing, err := s.repository.ExistingTenantMembers(
		ctx,
		tenantID,
		userIDs,
	)
	if err != nil {
		return err
	}

	if len(existing) != len(userIDs) {
		return ErrInvalidMembers
	}

	return nil
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

// normalizeAuthorizerError maps project authorization failures onto chat
// errors. A non-member of the project must see "forbidden", not "project
// not found".
func normalizeAuthorizerError(err error) error {
	switch {
	case errors.Is(err, project.ErrForbidden),
		errors.Is(err, project.ErrNotFound):
		return ErrForbidden

	case errors.Is(err, project.ErrInvalidID):
		return ErrInvalidID

	default:
		return err
	}
}

func dedupeUserIDs(values []string) []string {
	deduped := make([]string, 0, len(values))

	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}

		deduped = append(deduped, value)
	}

	return deduped
}

func removeUserID(values []string, userID string) []string {
	result := make([]string, 0, len(values))

	for _, value := range values {
		if value == userID {
			continue
		}

		result = append(result, value)
	}

	return result
}

func reverseMessages(items []Message) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func normalizeMessageBody(value string) (string, error) {
	body := strings.TrimSpace(value)

	if body == "" || utf8.RuneCountInString(body) > maxMessageBodyLength {
		return "", ErrInvalidBody
	}

	return body, nil
}

// normalizeOptionalMessageBody allows an empty body for attachment-only
// messages; the caller decides whether emptiness is acceptable.
func normalizeOptionalMessageBody(value string) (string, error) {
	body := strings.TrimSpace(value)

	if utf8.RuneCountInString(body) > maxMessageBodyLength {
		return "", ErrInvalidBody
	}

	return body, nil
}

func normalizeClientMessageID(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}

	id := strings.TrimSpace(*value)
	if id == "" {
		return nil, nil
	}

	if len(id) > maxClientMessageIDLength {
		return nil, ErrInvalidClientMessageID
	}

	return &id, nil
}

// sanitizeAttachmentFilename keeps the display name but removes path
// components, control characters and quotes (the filename is echoed into a
// Content-Disposition header when the file is downloaded).
func sanitizeAttachmentFilename(value string) (string, error) {
	filename := strings.TrimSpace(value)

	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = path.Base(filename)

	filename = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '/' {
			return -1
		}

		return r
	}, filename)

	filename = strings.TrimSpace(filename)

	if filename == "" ||
		filename == "." ||
		filename == ".." ||
		utf8.RuneCountInString(filename) > maxAttachmentFilenameLength {

		return "", ErrInvalidAttachmentFilename
	}

	return filename, nil
}

func normalizeAttachmentContentType(value string) string {
	contentType := strings.TrimSpace(value)

	contentType = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}

		return r
	}, contentType)

	if contentType == "" ||
		len(contentType) > maxAttachmentContentTypeLength {

		return "application/octet-stream"
	}

	return contentType
}

func normalizeAttachmentIDs(values []string) ([]string, error) {
	if len(values) > maxAttachmentsPerMessage {
		return nil, ErrTooManyAttachments
	}

	ids := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, raw := range values {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}

		if _, err := uuid.Parse(id); err != nil {
			return nil, ErrInvalidAttachment
		}

		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	return ids, nil
}

// IssueStreamToken mints a one-time WebSocket ticket. Browsers cannot set
// custom headers on a WebSocket handshake, so they fetch this over the normal
// authenticated REST API and pass the ticket as "?token=...".
func (s *Service) IssueStreamToken(
	ctx context.Context,
	identity auth.Identity,
) (StreamToken, error) {
	if identity.TenantID == "" || identity.UserID == "" {
		return StreamToken{}, fmt.Errorf(
			"issue stream token: missing identity",
		)
	}

	secret := make([]byte, streamTokenBytes)

	if _, err := rand.Read(secret); err != nil {
		return StreamToken{}, fmt.Errorf(
			"generate stream token: %w",
			err,
		)
	}

	hash := sha256.Sum256(secret)

	expiresAt, err := s.repository.CreateStreamToken(
		ctx,
		CreateStreamTokenParams{
			TokenHash: hash[:],

			TenantID: identity.TenantID,

			UserID: identity.UserID,

			TTL: streamTokenTTL,
		},
	)
	if err != nil {
		return StreamToken{}, err
	}

	return StreamToken{
		Token: base64.RawURLEncoding.EncodeToString(secret),

		ExpiresAt: expiresAt,
	}, nil
}

// ConsumeStreamToken validates and burns a ticket. The caller receives only
// the user and tenant: the ticket is a transport credential, not a substitute
// for a full identity profile.
func (s *Service) ConsumeStreamToken(
	ctx context.Context,
	raw string,
) (auth.Identity, error) {
	secret, err := decodeStreamToken(raw)
	if err != nil {
		return auth.Identity{}, err
	}

	hash := sha256.Sum256(secret)

	identity, err := s.repository.ConsumeStreamToken(ctx, hash[:])
	if err != nil {
		return auth.Identity{}, err
	}

	return auth.Identity{
		TenantID: identity.TenantID,

		UserID: identity.UserID,
	}, nil
}

func decodeStreamToken(raw string) ([]byte, error) {
	token := strings.TrimSpace(raw)

	if token == "" || len(token) > maxStreamTokenLength {
		return nil, ErrInvalidStreamToken
	}

	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != streamTokenBytes {
		return nil, ErrInvalidStreamToken
	}

	return secret, nil
}

// RunStreamTokenCleanup removes expired unused tickets until the context is
// canceled.
func (s *Service) RunStreamTokenCleanup(
	ctx context.Context,
	interval time.Duration,
) {
	s.cleanupStreamTokens(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			s.cleanupStreamTokens(ctx)
		}
	}
}

// CleanupStreamTokens deletes expired tickets. It is exported for the startup
// worker and tests.
func (s *Service) CleanupStreamTokens(
	ctx context.Context,
) (int64, error) {
	return s.repository.DeleteExpiredStreamTokens(ctx)
}

func (s *Service) cleanupStreamTokens(ctx context.Context) {
	deleted, err := s.CleanupStreamTokens(ctx)
	if err != nil {
		log.Printf("cleanup chat stream tokens: %v", err)
		return
	}

	if deleted > 0 {
		log.Printf("removed %d expired chat stream tokens", deleted)
	}
}
