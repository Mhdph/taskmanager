package chat

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

const (
	maxGroupTitleLength = 200

	maxGroupMembers = 50

	defaultMessagePageSize = 50
	maxMessagePageSize     = 100

	maxConversationPageSize = 100

	maxMessageBodyLength = 10000

	maxClientMessageIDLength = 128
)

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
}

func NewService(
	repository *Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
	publisher Publisher,
) *Service {
	return &Service{
		repository: repository,

		projectRepository: projectRepository,

		projectAuthorizer: projectAuthorizer,

		publisher: publisher,
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
}

// SendMessage persists a user message. The bool reports whether a new row
// was created; false means the client message id was already stored and the
// original message is returned.
func (s *Service) SendMessage(
	ctx context.Context,
	input SendMessageInput,
) (Message, bool, error) {
	body, err := normalizeMessageBody(input.Body)
	if err != nil {
		return Message{}, false, err
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
		},
	)
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

	return s.repository.DeleteMessage(
		ctx,
		input.Identity.TenantID,
		conversation.ID,
		message.ID,
	)
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
