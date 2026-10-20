package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

type Repository struct {
	db      *pgxpool.Pool
	queries *db.Queries
}

func NewRepository(
	pool *pgxpool.Pool,
	queries *db.Queries,
) *Repository {
	return &Repository{
		db:      pool,
		queries: queries,
	}
}

// -------------------------------------------------------------------------
// Conversations
// -------------------------------------------------------------------------

type CreateProjectConversationParams struct {
	ID       string
	TenantID string

	ProjectID string

	CreatedByUserID string
}

// CreateProjectConversation returns the project's chat, creating it on the
// first call.
func (r *Repository) CreateProjectConversation(
	ctx context.Context,
	params CreateProjectConversationParams,
) (Conversation, error) {
	id, err := parseUUID(params.ID)
	if err != nil {
		return Conversation{}, fmt.Errorf("parse conversation id: %w", err)
	}

	projectID, err := parseUUID(params.ProjectID)
	if err != nil {
		return Conversation{}, fmt.Errorf("parse project id: %w", err)
	}

	row, err := r.queries.CreateProjectConversation(
		ctx,
		db.CreateProjectConversationParams{
			ID:              id,
			TenantID:        params.TenantID,
			ProjectID:       projectID,
			CreatedByUserID: params.CreatedByUserID,
		},
	)
	if err != nil {
		return Conversation{}, fmt.Errorf("create project conversation: %w", err)
	}

	return conversationFromDB(row), nil
}

type CreateGroupConversationParams struct {
	ID       string
	TenantID string

	Title string

	CreatedByUserID string

	// MemberIDs excludes the creator, who is always added as the owner.
	MemberIDs []string
}

func (r *Repository) CreateGroupConversation(
	ctx context.Context,
	params CreateGroupConversationParams,
) (Conversation, error) {
	id, err := parseUUID(params.ID)
	if err != nil {
		return Conversation{}, fmt.Errorf("parse conversation id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Conversation{}, fmt.Errorf("begin group conversation transaction: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	row, err := qtx.CreateGroupConversation(
		ctx,
		db.CreateGroupConversationParams{
			ID:              id,
			TenantID:        params.TenantID,
			Title:           params.Title,
			CreatedByUserID: params.CreatedByUserID,
		},
	)
	if err != nil {
		return Conversation{}, fmt.Errorf("create group conversation: %w", err)
	}

	if err := qtx.AddConversationMember(
		ctx,
		db.AddConversationMemberParams{
			ConversationID: id,
			TenantID:       params.TenantID,
			UserID:         params.CreatedByUserID,
			Role:           string(MemberRoleOwner),
		},
	); err != nil {
		return Conversation{}, fmt.Errorf("add conversation owner: %w", err)
	}

	for _, userID := range params.MemberIDs {
		if err := qtx.AddConversationMember(
			ctx,
			db.AddConversationMemberParams{
				ConversationID: id,
				TenantID:       params.TenantID,
				UserID:         userID,
				Role:           string(MemberRoleMember),
			},
		); err != nil {
			return Conversation{}, fmt.Errorf("add conversation member: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, fmt.Errorf("commit group conversation: %w", err)
	}

	return conversationFromDB(row), nil
}

// CreateDMConversation returns the canonical conversation for the user
// pair. The caller is responsible for passing two distinct workspace
// members.
func (r *Repository) CreateDMConversation(
	ctx context.Context,
	tenantID string,
	initiatorUserID string,
	otherUserID string,
	createdByUserID string,
	newID string,
) (Conversation, error) {
	id, err := parseUUID(newID)
	if err != nil {
		return Conversation{}, fmt.Errorf("parse conversation id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Conversation{}, fmt.Errorf("begin dm transaction: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	row, err := qtx.CreateDMConversation(
		ctx,
		db.CreateDMConversationParams{
			ID:              id,
			TenantID:        tenantID,
			DmKey:           pgtype.Text{String: dmKey(initiatorUserID, otherUserID), Valid: true},
			CreatedByUserID: createdByUserID,
		},
	)
	if err != nil {
		return Conversation{}, fmt.Errorf("create dm conversation: %w", err)
	}

	for _, userID := range []string{initiatorUserID, otherUserID} {
		if err := qtx.AddConversationMember(
			ctx,
			db.AddConversationMemberParams{
				ConversationID: row.ID,
				TenantID:       tenantID,
				UserID:         userID,
				Role:           string(MemberRoleMember),
			},
		); err != nil {
			return Conversation{}, fmt.Errorf("add dm member: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Conversation{}, fmt.Errorf("commit dm conversation: %w", err)
	}

	return conversationFromDB(row), nil
}

func (r *Repository) GetConversation(
	ctx context.Context,
	tenantID string,
	conversationID string,
) (Conversation, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return Conversation{}, fmt.Errorf("parse conversation id: %w", err)
	}

	row, err := r.queries.GetConversation(
		ctx,
		db.GetConversationParams{
			TenantID: tenantID,
			ID:       id,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, ErrConversationNotFound
		}

		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}

	return conversationFromDB(row), nil
}

func (r *Repository) GetProjectConversation(
	ctx context.Context,
	tenantID string,
	projectID string,
) (Conversation, error) {
	id, err := parseUUID(projectID)
	if err != nil {
		return Conversation{}, fmt.Errorf("parse project id: %w", err)
	}

	row, err := r.queries.GetProjectConversation(
		ctx,
		db.GetProjectConversationParams{
			TenantID:  tenantID,
			ProjectID: id,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Conversation{}, ErrConversationNotFound
		}

		return Conversation{}, fmt.Errorf("get project conversation: %w", err)
	}

	return conversationFromDB(row), nil
}

// ListForUser returns the conversations visible to the user, ordered by
// last activity. Members are loaded in one batch, not per conversation.
func (r *Repository) ListForUser(
	ctx context.Context,
	tenantID string,
	userID string,
	isAdmin bool,
	limit int32,
) ([]ConversationSummary, error) {
	rows, err := r.queries.ListConversationsForUser(
		ctx,
		db.ListConversationsForUserParams{
			UserID:    userID,
			TenantID:  tenantID,
			IsAdmin:   isAdmin,
			PageLimit: limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}

	items := make([]ConversationSummary, 0, len(rows))
	conversationIDs := make([]pgtype.UUID, 0, len(rows))

	for _, row := range rows {
		summary := ConversationSummary{
			Conversation: conversationFromListRow(row),
			MemberRole:   MemberRole(row.MemberRole),
			LastReadSeq:  row.LastReadSeq,
			UnreadCount:  row.UnreadCount,
		}

		if row.LastMessageCreatedAt.Valid {
			preview := MessagePreview{
				Kind:      MessageKind(row.LastMessageKind),
				Body:      row.LastMessageBody,
				CreatedAt: row.LastMessageCreatedAt.Time,
			}

			if row.LastMessageAuthorUserID.Valid {
				author := row.LastMessageAuthorUserID.String
				preview.AuthorUserID = &author
			}

			summary.LastMessage = &preview
		}

		items = append(items, summary)
		conversationIDs = append(conversationIDs, row.ID)
	}

	if len(conversationIDs) == 0 {
		return items, nil
	}

	membersByConversation, err := r.listMembersByConversation(ctx, tenantID, conversationIDs)
	if err != nil {
		return nil, err
	}

	for i := range items {
		members := membersByConversation[items[i].ID]

		ids := make([]string, 0, len(members))
		for _, member := range members {
			ids = append(ids, member.UserID)
		}

		items[i].MemberIDs = ids
	}

	return items, nil
}

func (r *Repository) listMembersByConversation(
	ctx context.Context,
	tenantID string,
	conversationIDs []pgtype.UUID,
) (map[string][]Member, error) {
	rows, err := r.queries.ListConversationMembers(
		ctx,
		db.ListConversationMembersParams{
			TenantID:        tenantID,
			ConversationIds: conversationIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list conversation members: %w", err)
	}

	result := make(map[string][]Member, len(conversationIDs))

	for _, row := range rows {
		conversationID := uuid.UUID(row.ConversationID.Bytes).String()
		result[conversationID] = append(result[conversationID], memberFromDB(row))
	}

	return result, nil
}

// -------------------------------------------------------------------------
// Members
// -------------------------------------------------------------------------

func (r *Repository) ListMembers(
	ctx context.Context,
	tenantID string,
	conversationID string,
) ([]Member, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}

	membersByConversation, err := r.listMembersByConversation(
		ctx,
		tenantID,
		[]pgtype.UUID{id},
	)
	if err != nil {
		return nil, err
	}

	members := membersByConversation[conversationID]
	if members == nil {
		members = []Member{}
	}

	return members, nil
}

func (r *Repository) GetMember(
	ctx context.Context,
	tenantID string,
	conversationID string,
	userID string,
) (Member, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return Member{}, fmt.Errorf("parse conversation id: %w", err)
	}

	row, err := r.queries.GetConversationMember(
		ctx,
		db.GetConversationMemberParams{
			TenantID:       tenantID,
			ConversationID: id,
			UserID:         userID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, ErrMemberNotFound
		}

		return Member{}, fmt.Errorf("get conversation member: %w", err)
	}

	return memberFromDB(row), nil
}

func (r *Repository) AddMember(
	ctx context.Context,
	tenantID string,
	conversationID string,
	userID string,
	role MemberRole,
) error {
	id, err := parseUUID(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}

	if err := r.queries.AddConversationMember(
		ctx,
		db.AddConversationMemberParams{
			ConversationID: id,
			TenantID:       tenantID,
			UserID:         userID,
			Role:           string(role),
		},
	); err != nil {
		return fmt.Errorf("add conversation member: %w", err)
	}

	return nil
}

func (r *Repository) RemoveMember(
	ctx context.Context,
	tenantID string,
	conversationID string,
	userID string,
) (bool, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return false, fmt.Errorf("parse conversation id: %w", err)
	}

	affected, err := r.queries.RemoveConversationMember(
		ctx,
		db.RemoveConversationMemberParams{
			TenantID:       tenantID,
			ConversationID: id,
			UserID:         userID,
		},
	)
	if err != nil {
		return false, fmt.Errorf("remove conversation member: %w", err)
	}

	return affected > 0, nil
}

// SyncProjectMembers mirrors project membership into the conversation in
// one transaction, then prunes project-sourced rows for users who left.
func (r *Repository) SyncProjectMembers(
	ctx context.Context,
	tenantID string,
	conversationID string,
	projectID string,
) error {
	conversationUUID, err := parseUUID(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}

	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return fmt.Errorf("parse project id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin membership sync: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if err := qtx.SyncProjectConversationMembers(
		ctx,
		db.SyncProjectConversationMembersParams{
			ConversationID: conversationUUID,
			TenantID:       tenantID,
			ProjectID:      projectUUID,
		},
	); err != nil {
		return fmt.Errorf("sync conversation members: %w", err)
	}

	if err := qtx.PruneProjectConversationMembers(
		ctx,
		db.PruneProjectConversationMembersParams{
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			ProjectID:      projectUUID,
		},
	); err != nil {
		return fmt.Errorf("prune conversation members: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit membership sync: %w", err)
	}

	return nil
}

// ExistingTenantMembers returns the subset of userIDs that belong to the
// workspace.
func (r *Repository) ExistingTenantMembers(
	ctx context.Context,
	tenantID string,
	userIDs []string,
) ([]string, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	ids, err := r.queries.ListChatTenantMemberIDsIn(
		ctx,
		db.ListChatTenantMemberIDsInParams{
			TenantID: tenantID,
			UserIds:  userIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace members: %w", err)
	}

	return ids, nil
}

func (r *Repository) UpdateReadCursor(
	ctx context.Context,
	tenantID string,
	conversationID string,
	userID string,
	lastReadSeq int64,
) (bool, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return false, fmt.Errorf("parse conversation id: %w", err)
	}

	affected, err := r.queries.UpdateConversationReadCursor(
		ctx,
		db.UpdateConversationReadCursorParams{
			LastReadSeq:    lastReadSeq,
			TenantID:       tenantID,
			ConversationID: id,
			UserID:         userID,
		},
	)
	if err != nil {
		return false, fmt.Errorf("update read cursor: %w", err)
	}

	return affected > 0, nil
}

// -------------------------------------------------------------------------
// Messages
// -------------------------------------------------------------------------

type CreateMessageParams struct {
	ID       string
	TenantID string

	ConversationID string

	Kind MessageKind

	// AuthorUserID is nil for system messages.
	AuthorUserID *string

	Body string

	// ClientMessageID enables idempotent retries.
	ClientMessageID *string
}

// CreateMessage inserts a message and advances the conversation tail.
// The returned bool reports whether the row was created; false means the
// client message id was already stored and the original message is
// returned unchanged (idempotent retry).
func (r *Repository) CreateMessage(
	ctx context.Context,
	params CreateMessageParams,
) (Message, bool, error) {
	messageID, err := parseUUID(params.ID)
	if err != nil {
		return Message{}, false, fmt.Errorf("parse message id: %w", err)
	}

	conversationID, err := parseUUID(params.ConversationID)
	if err != nil {
		return Message{}, false, fmt.Errorf("parse conversation id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Message{}, false, fmt.Errorf("begin message transaction: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	row, err := qtx.CreateChatMessage(
		ctx,
		db.CreateChatMessageParams{
			ID:              messageID,
			TenantID:        params.TenantID,
			ConversationID:  conversationID,
			Kind:            string(params.Kind),
			AuthorUserID:    textValue(params.AuthorUserID),
			Body:            params.Body,
			ClientMessageID: textValue(params.ClientMessageID),
		},
	)

	if errors.Is(err, pgx.ErrNoRows) {
		if params.ClientMessageID == nil || params.AuthorUserID == nil {
			return Message{}, false, fmt.Errorf(
				"create message: %w",
				ErrMessageNotFound,
			)
		}

		existing, getErr := qtx.GetChatMessageByClientID(
			ctx,
			db.GetChatMessageByClientIDParams{
				TenantID:        params.TenantID,
				AuthorUserID:    textValue(params.AuthorUserID),
				ClientMessageID: textValue(params.ClientMessageID),
			},
		)
		if getErr != nil {
			return Message{}, false, fmt.Errorf("get idempotent message: %w", getErr)
		}

		if err := tx.Commit(ctx); err != nil {
			return Message{}, false, fmt.Errorf("commit idempotent message: %w", err)
		}

		return messageFromDB(existing), false, nil
	}

	if err != nil {
		return Message{}, false, fmt.Errorf("create message: %w", err)
	}

	if err := qtx.BumpConversationLastMessage(
		ctx,
		db.BumpConversationLastMessageParams{
			LastMessageSeq: row.Seq,
			LastMessageAt: pgtype.Timestamptz{
				Time:  row.CreatedAt.Time,
				Valid: true,
			},
			TenantID: params.TenantID,
			ID:       conversationID,
		},
	); err != nil {
		return Message{}, false, fmt.Errorf("bump conversation tail: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Message{}, false, fmt.Errorf("commit message: %w", err)
	}

	return messageFromDB(row), true, nil
}

// ListMessages returns up to limit messages older than beforeSeq
// (newest first). A nil beforeSeq starts from the newest message.
func (r *Repository) ListMessages(
	ctx context.Context,
	tenantID string,
	conversationID string,
	beforeSeq *int64,
	limit int32,
) ([]Message, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}

	var before pgtype.Int8
	if beforeSeq != nil {
		before = pgtype.Int8{Int64: *beforeSeq, Valid: true}
	}

	rows, err := r.queries.ListChatMessages(
		ctx,
		db.ListChatMessagesParams{
			TenantID:       tenantID,
			ConversationID: id,
			BeforeSeq:      before,
			PageLimit:      limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	items := make([]Message, 0, len(rows))
	for _, row := range rows {
		items = append(items, messageFromDB(row))
	}

	return items, nil
}

func (r *Repository) GetMessage(
	ctx context.Context,
	tenantID string,
	conversationID string,
	messageID string,
) (Message, error) {
	conversationUUID, err := parseUUID(conversationID)
	if err != nil {
		return Message{}, fmt.Errorf("parse conversation id: %w", err)
	}

	messageUUID, err := parseUUID(messageID)
	if err != nil {
		return Message{}, fmt.Errorf("parse message id: %w", err)
	}

	row, err := r.queries.GetChatMessage(
		ctx,
		db.GetChatMessageParams{
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			ID:             messageUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, ErrMessageNotFound
		}

		return Message{}, fmt.Errorf("get message: %w", err)
	}

	return messageFromDB(row), nil
}

func (r *Repository) EditMessage(
	ctx context.Context,
	tenantID string,
	conversationID string,
	messageID string,
	body string,
) (Message, error) {
	conversationUUID, err := parseUUID(conversationID)
	if err != nil {
		return Message{}, fmt.Errorf("parse conversation id: %w", err)
	}

	messageUUID, err := parseUUID(messageID)
	if err != nil {
		return Message{}, fmt.Errorf("parse message id: %w", err)
	}

	row, err := r.queries.EditChatMessage(
		ctx,
		db.EditChatMessageParams{
			Body:           body,
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			ID:             messageUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, ErrMessageNotFound
		}

		return Message{}, fmt.Errorf("edit message: %w", err)
	}

	return messageFromDB(row), nil
}

func (r *Repository) DeleteMessage(
	ctx context.Context,
	tenantID string,
	conversationID string,
	messageID string,
) (Message, error) {
	conversationUUID, err := parseUUID(conversationID)
	if err != nil {
		return Message{}, fmt.Errorf("parse conversation id: %w", err)
	}

	messageUUID, err := parseUUID(messageID)
	if err != nil {
		return Message{}, fmt.Errorf("parse message id: %w", err)
	}

	row, err := r.queries.DeleteChatMessage(
		ctx,
		db.DeleteChatMessageParams{
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			ID:             messageUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, ErrMessageNotFound
		}

		return Message{}, fmt.Errorf("delete message: %w", err)
	}

	return messageFromDB(row), nil
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

func parseUUID(value string) (pgtype.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%w: %v", ErrInvalidID, err)
	}

	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func textValue(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}

	return pgtype.Text{String: *value, Valid: true}
}

// dmKey canonically identifies a user pair regardless of who started the
// conversation.
func dmKey(first string, second string) string {
	if first > second {
		first, second = second, first
	}

	return first + ":" + second
}

func conversationFromDB(row db.Conversation) Conversation {
	conversation := Conversation{
		ID:              uuid.UUID(row.ID.Bytes).String(),
		TenantID:        row.TenantID,
		Type:            ConversationType(row.Type),
		Title:           row.Title,
		CreatedByUserID: row.CreatedByUserID,
		LastMessageSeq:  row.LastMessageSeq,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}

	if row.ProjectID.Valid {
		value := uuid.UUID(row.ProjectID.Bytes).String()
		conversation.ProjectID = &value
	}

	if row.LastMessageAt.Valid {
		value := row.LastMessageAt.Time
		conversation.LastMessageAt = &value
	}

	return conversation
}

func conversationFromListRow(row db.ListConversationsForUserRow) Conversation {
	conversation := Conversation{
		ID:              uuid.UUID(row.ID.Bytes).String(),
		TenantID:        row.TenantID,
		Type:            ConversationType(row.Type),
		Title:           row.Title,
		CreatedByUserID: row.CreatedByUserID,
		LastMessageSeq:  row.LastMessageSeq,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}

	if row.ProjectID.Valid {
		value := uuid.UUID(row.ProjectID.Bytes).String()
		conversation.ProjectID = &value
	}

	if row.LastMessageAt.Valid {
		value := row.LastMessageAt.Time
		conversation.LastMessageAt = &value
	}

	return conversation
}

func memberFromDB(row db.ConversationMember) Member {
	return Member{
		UserID:      row.UserID,
		Role:        MemberRole(row.Role),
		Source:      MemberSource(row.Source),
		LastReadSeq: row.LastReadSeq,
		JoinedAt:    row.JoinedAt.Time,
	}
}

func messageFromDB(row db.Message) Message {
	message := Message{
		ID:             uuid.UUID(row.ID.Bytes).String(),
		TenantID:       row.TenantID,
		ConversationID: uuid.UUID(row.ConversationID.Bytes).String(),
		Seq:            row.Seq,
		Kind:           MessageKind(row.Kind),
		Body:           row.Body,
		CreatedAt:      row.CreatedAt.Time,
	}

	if row.AuthorUserID.Valid {
		value := row.AuthorUserID.String
		message.AuthorUserID = &value
	}

	if row.ClientMessageID.Valid {
		value := row.ClientMessageID.String
		message.ClientMessageID = &value
	}

	if row.EditedAt.Valid {
		value := row.EditedAt.Time
		message.EditedAt = &value
	}

	if row.DeletedAt.Valid {
		value := row.DeletedAt.Time
		message.DeletedAt = &value
	}

	return message
}
