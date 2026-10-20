package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
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
}

func NewService(
	repository *Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
	publisher Publisher,
	fileStorage FileStorage,
	maxUploadSize int64,
) *Service {
	return &Service{
		repository: repository,

		projectRepository: projectRepository,

		projectAuthorizer: projectAuthorizer,

		publisher: publisher,

		fileStorage: fileStorage,

		maxUploadSize: maxUploadSize,
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

	// Before is an opaque cursor from a previous page.
	Before string

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

	return s.repository.CreateMessage(
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
