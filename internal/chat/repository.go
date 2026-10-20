package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

type Repository struct {
	db      *pgxpool.Pool
	queries *db.Queries

	// publisher may be nil (unit tests, read-only tooling): writes still
	// commit, they just do not fan out.
	publisher EventPublisher
}

func NewRepository(
	pool *pgxpool.Pool,
	queries *db.Queries,
	publisher EventPublisher,
) *Repository {
	return &Repository{
		db:      pool,
		queries: queries,

		publisher: publisher,
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

func (r *Repository) ListConversationIDsForUser(
	ctx context.Context,
	tenantID string,
	userID string,
) ([]string, error) {
	ids, err := r.queries.ListChatConversationIDsForUser(
		ctx,
		db.ListChatConversationIDsForUserParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list user conversations: %w", err)
	}

	return ids, nil
}

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

// resolveMentions returns the members mentioned in body. The result is
// never nil so it can be stored in a NOT NULL array column.
func (r *Repository) resolveMentions(
	ctx context.Context,
	qtx *db.Queries,
	conversationID string,
	body string,
) ([]string, error) {
	memberUserIDs, err := r.listMemberUserIDs(ctx, qtx, conversationID)
	if err != nil {
		return nil, err
	}

	mentions := parseMentions(body, memberUserIDs)
	if mentions == nil {
		mentions = []string{}
	}

	return mentions, nil
}

func (r *Repository) listMemberUserIDs(
	ctx context.Context,
	qtx *db.Queries,
	conversationID string,
) ([]string, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}

	userIDs, err := qtx.ListChatMemberUserIDs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list conversation member ids: %w", err)
	}

	return userIDs, nil
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

	// AttachmentIDs binds previously uploaded pending attachments to this
	// message inside the same transaction.
	AttachmentIDs []string
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

	// Serialize inserts per conversation so sequence assignment order
	// matches commit order. This is what makes "after=<last seq>" replay
	// safe: a lower seq cannot become visible after a higher one.
	// Use the canonical UUID form so the same conversation cannot take
	// different locks through upper/lower-case or braced path input.
	lockKey := params.TenantID + ":" + uuid.UUID(conversationID.Bytes).String()

	if err := qtx.LockChatConversation(ctx, lockKey); err != nil {
		return Message{}, false, fmt.Errorf(
			"lock conversation messages: %w",
			err,
		)
	}

	// Mentions are resolved once, against the members that exist when the
	// message is written, and stored with it. Notification routing and
	// clients then read the same list.
	mentions, err := r.resolveMentions(
		ctx,
		qtx,
		params.ConversationID,
		params.Body,
	)
	if err != nil {
		return Message{}, false, err
	}

	row, err := qtx.CreateChatMessage(
		ctx,
		db.CreateChatMessageParams{
			ID:               messageID,
			TenantID:         params.TenantID,
			ConversationID:   conversationID,
			Kind:             string(params.Kind),
			AuthorUserID:     textValue(params.AuthorUserID),
			Body:             params.Body,
			ClientMessageID:  textValue(params.ClientMessageID),
			MentionedUserIds: mentions,
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
				ConversationID:  conversationID,
				AuthorUserID:    textValue(params.AuthorUserID),
				ClientMessageID: textValue(params.ClientMessageID),
			},
		)
		if getErr != nil {
			return Message{}, false, fmt.Errorf("get idempotent message: %w", getErr)
		}

		existingMessage := messageFromDB(existing)

		existingAttachments, attErr := r.listMessageAttachments(
			ctx,
			qtx,
			params.TenantID,
			params.ConversationID,
			existingMessage.ID,
		)
		if attErr != nil {
			return Message{}, false, attErr
		}

		existingMessage.Attachments = existingAttachments

		if err := tx.Commit(ctx); err != nil {
			return Message{}, false, fmt.Errorf("commit idempotent message: %w", err)
		}

		return existingMessage, false, nil
	}

	if err != nil {
		return Message{}, false, fmt.Errorf("create message: %w", err)
	}

	createdMessageID := uuid.UUID(row.ID.Bytes).String()

	attachments, err := r.bindMessageAttachments(
		ctx,
		qtx,
		params,
		createdMessageID,
	)
	if err != nil {
		return Message{}, false, err
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

	// NOTIFY is delivered only on commit, so the event can never describe
	// a message that was rolled back.
	if err := r.publishTx(ctx, tx, Event{
		Kind:           EventMessageCreated,
		TenantID:       params.TenantID,
		ConversationID: params.ConversationID,
		MessageID:      createdMessageID,
		Seq:            row.Seq,
		ActorUserID:    stringValue(params.AuthorUserID),
	}); err != nil {
		return Message{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Message{}, false, fmt.Errorf("commit message: %w", err)
	}

	message := messageFromDB(row)
	message.Attachments = attachments

	return message, true, nil
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

	return r.hydrateMessageRows(ctx, tenantID, id, rows)
}

// ListMessagesAfter returns up to limit messages newer than afterSeq, oldest
// first. The caller pages forward by passing the last returned seq.
func (r *Repository) ListMessagesAfter(
	ctx context.Context,
	tenantID string,
	conversationID string,
	afterSeq int64,
	limit int32,
) ([]Message, error) {
	id, err := parseUUID(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}

	rows, err := r.queries.ListChatMessagesAfter(
		ctx,
		db.ListChatMessagesAfterParams{
			TenantID:       tenantID,
			ConversationID: id,
			AfterSeq:       afterSeq,
			PageLimit:      limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list messages after: %w", err)
	}

	return r.hydrateMessageRows(ctx, tenantID, id, rows)
}

func (r *Repository) hydrateMessageRows(
	ctx context.Context,
	tenantID string,
	conversationUUID pgtype.UUID,
	rows []db.Message,
) ([]Message, error) {
	items := make([]Message, 0, len(rows))
	for _, row := range rows {
		items = append(items, messageFromDB(row))
	}

	if err := r.hydrateMessageAttachments(
		ctx,
		r.queries,
		tenantID,
		conversationUUID,
		items,
	); err != nil {
		return nil, err
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

	message := messageFromDB(row)

	attachments, err := r.listMessageAttachments(
		ctx,
		r.queries,
		tenantID,
		conversationID,
		message.ID,
	)
	if err != nil {
		return Message{}, err
	}

	message.Attachments = attachments

	return message, nil
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

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("begin message edit: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	// An edit can add or remove mentions, so the stored list is resolved
	// again against the current members.
	mentions, err := r.resolveMentions(
		ctx,
		qtx,
		conversationID,
		body,
	)
	if err != nil {
		return Message{}, err
	}

	row, err := qtx.EditChatMessage(
		ctx,
		db.EditChatMessageParams{
			Body:             body,
			MentionedUserIds: mentions,
			TenantID:         tenantID,
			ConversationID:   conversationUUID,
			ID:               messageUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, ErrMessageNotFound
		}

		return Message{}, fmt.Errorf("edit message: %w", err)
	}

	if err := r.publishTx(ctx, tx, Event{
		Kind:           EventMessageEdited,
		TenantID:       tenantID,
		ConversationID: conversationID,
		MessageID:      messageID,
		Seq:            row.Seq,
		ActorUserID:    textStringValue(row.AuthorUserID),
	}); err != nil {
		return Message{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Message{}, fmt.Errorf("commit message edit: %w", err)
	}

	return messageFromDB(row), nil
}

// DeleteMessage tombstones a message and its attachments. The returned
// attachments are already soft-deleted; the caller is responsible for
// removing the storage objects.
func (r *Repository) DeleteMessage(
	ctx context.Context,
	tenantID string,
	conversationID string,
	messageID string,
) (Message, []Attachment, error) {
	conversationUUID, err := parseUUID(conversationID)
	if err != nil {
		return Message{}, nil, fmt.Errorf("parse conversation id: %w", err)
	}

	messageUUID, err := parseUUID(messageID)
	if err != nil {
		return Message{}, nil, fmt.Errorf("parse message id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Message{}, nil, fmt.Errorf("begin message delete: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	row, err := qtx.DeleteChatMessage(
		ctx,
		db.DeleteChatMessageParams{
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			ID:             messageUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, nil, ErrMessageNotFound
		}

		return Message{}, nil, fmt.Errorf("delete message: %w", err)
	}

	attachmentRows, err := qtx.SoftDeleteChatMessageAttachments(
		ctx,
		db.SoftDeleteChatMessageAttachmentsParams{
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			MessageID:      messageUUID,
		},
	)
	if err != nil {
		return Message{}, nil, fmt.Errorf(
			"delete message attachments: %w",
			err,
		)
	}

	if err := r.publishTx(ctx, tx, Event{
		Kind:           EventMessageDeleted,
		TenantID:       tenantID,
		ConversationID: conversationID,
		MessageID:      messageID,
		Seq:            row.Seq,
		ActorUserID:    textStringValue(row.AuthorUserID),
	}); err != nil {
		return Message{}, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Message{}, nil, fmt.Errorf("commit message delete: %w", err)
	}

	return messageFromDB(row), attachmentsFromDB(attachmentRows), nil
}

// -------------------------------------------------------------------------
// Attachments
// -------------------------------------------------------------------------

type CreateAttachmentParams struct {
	ID string

	TenantID string

	ConversationID string

	UploadedByUserID string

	Filename string

	ContentType string

	SizeBytes int64

	ObjectKey string
}

func (r *Repository) CreateAttachment(
	ctx context.Context,
	params CreateAttachmentParams,
) (Attachment, error) {
	id, err := parseUUID(params.ID)
	if err != nil {
		return Attachment{}, fmt.Errorf("parse attachment id: %w", err)
	}

	conversationID, err := parseUUID(params.ConversationID)
	if err != nil {
		return Attachment{}, fmt.Errorf("parse conversation id: %w", err)
	}

	row, err := r.queries.CreateChatAttachment(
		ctx,
		db.CreateChatAttachmentParams{
			ID:               id,
			TenantID:         params.TenantID,
			ConversationID:   conversationID,
			UploadedByUserID: params.UploadedByUserID,
			Filename:         params.Filename,
			ContentType:      params.ContentType,
			SizeBytes:        params.SizeBytes,
			ObjectKey:        params.ObjectKey,
		},
	)
	if err != nil {
		return Attachment{}, fmt.Errorf("create attachment: %w", err)
	}

	return attachmentFromDB(row), nil
}

func (r *Repository) GetAttachment(
	ctx context.Context,
	tenantID string,
	conversationID string,
	attachmentID string,
) (Attachment, error) {
	conversationUUID, err := parseUUID(conversationID)
	if err != nil {
		return Attachment{}, fmt.Errorf("parse conversation id: %w", err)
	}

	attachmentUUID, err := parseUUID(attachmentID)
	if err != nil {
		return Attachment{}, fmt.Errorf("parse attachment id: %w", err)
	}

	row, err := r.queries.GetChatAttachment(
		ctx,
		db.GetChatAttachmentParams{
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			ID:             attachmentUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Attachment{}, ErrAttachmentNotFound
		}

		return Attachment{}, fmt.Errorf("get attachment: %w", err)
	}

	return attachmentFromDB(row), nil
}

// ListStalePendingAttachments returns uploads that were never bound to a
// message.
func (r *Repository) ListStalePendingAttachments(
	ctx context.Context,
	createdBefore time.Time,
	limit int32,
) ([]Attachment, error) {
	rows, err := r.queries.ListStalePendingChatAttachments(
		ctx,
		db.ListStalePendingChatAttachmentsParams{
			CreatedBefore: pgtype.Timestamptz{
				Time:  createdBefore,
				Valid: true,
			},
			PageLimit: limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list stale attachments: %w", err)
	}

	return attachmentsFromDB(rows), nil
}

// DeletePendingAttachment removes an upload that is not bound to a message.
// The condition is enforced in SQL, so a concurrent send that attaches the
// row first wins and this returns false.
func (r *Repository) DeletePendingAttachment(
	ctx context.Context,
	tenantID string,
	conversationID string,
	attachmentID string,
	uploadedByUserID string,
) (bool, error) {
	conversationUUID, err := parseUUID(conversationID)
	if err != nil {
		return false, fmt.Errorf("parse conversation id: %w", err)
	}

	attachmentUUID, err := parseUUID(attachmentID)
	if err != nil {
		return false, fmt.Errorf("parse attachment id: %w", err)
	}

	deleted, err := r.queries.DeletePendingChatAttachment(
		ctx,
		db.DeletePendingChatAttachmentParams{
			TenantID:         tenantID,
			ConversationID:   conversationUUID,
			ID:               attachmentUUID,
			UploadedByUserID: uploadedByUserID,
		},
	)
	if err != nil {
		return false, fmt.Errorf("delete pending attachment: %w", err)
	}

	return deleted > 0, nil
}

// bindMessageAttachments binds pending uploads inside the message
// transaction. Every requested id must belong to this conversation, this
// author, and still be pending; otherwise the whole send rolls back.
func (r *Repository) bindMessageAttachments(
	ctx context.Context,
	q *db.Queries,
	params CreateMessageParams,
	messageID string,
) ([]Attachment, error) {
	if len(params.AttachmentIDs) == 0 {
		return []Attachment{}, nil
	}

	messageUUID, err := parseUUID(messageID)
	if err != nil {
		return nil, fmt.Errorf("parse message id: %w", err)
	}

	conversationUUID, err := parseUUID(params.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}

	attachmentIDs := make(
		[]pgtype.UUID,
		0,
		len(params.AttachmentIDs),
	)

	for _, raw := range params.AttachmentIDs {
		id, err := parseUUID(raw)
		if err != nil {
			return nil, ErrInvalidAttachment
		}

		attachmentIDs = append(attachmentIDs, id)
	}

	attached, err := q.AttachChatAttachmentsToMessage(
		ctx,
		db.AttachChatAttachmentsToMessageParams{
			MessageID:        messageUUID,
			TenantID:         params.TenantID,
			ConversationID:   conversationUUID,
			UploadedByUserID: stringValue(params.AuthorUserID),
			AttachmentIds:    attachmentIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("attach message attachments: %w", err)
	}

	if attached != int64(len(attachmentIDs)) {
		return nil, ErrInvalidAttachment
	}

	return r.listMessageAttachments(
		ctx,
		q,
		params.TenantID,
		params.ConversationID,
		messageID,
	)
}

func (r *Repository) listMessageAttachments(
	ctx context.Context,
	q *db.Queries,
	tenantID string,
	conversationID string,
	messageID string,
) ([]Attachment, error) {
	messageUUID, err := parseUUID(messageID)
	if err != nil {
		return nil, fmt.Errorf("parse message id: %w", err)
	}

	conversationUUID, err := parseUUID(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}

	rows, err := q.ListChatAttachmentsByMessages(
		ctx,
		db.ListChatAttachmentsByMessagesParams{
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			MessageIds:     []pgtype.UUID{messageUUID},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list message attachments: %w", err)
	}

	return attachmentsFromDB(rows), nil
}

func (r *Repository) hydrateMessageAttachments(
	ctx context.Context,
	q *db.Queries,
	tenantID string,
	conversationUUID pgtype.UUID,
	items []Message,
) error {
	if len(items) == 0 {
		return nil
	}

	messageIDs := make([]pgtype.UUID, 0, len(items))

	for _, item := range items {
		id, err := parseUUID(item.ID)
		if err != nil {
			return fmt.Errorf("parse message id: %w", err)
		}

		messageIDs = append(messageIDs, id)
	}

	rows, err := q.ListChatAttachmentsByMessages(
		ctx,
		db.ListChatAttachmentsByMessagesParams{
			TenantID:       tenantID,
			ConversationID: conversationUUID,
			MessageIds:     messageIDs,
		},
	)
	if err != nil {
		return fmt.Errorf("list message attachments: %w", err)
	}

	grouped := make(map[string][]Attachment, len(items))

	for _, row := range rows {
		messageID := uuid.UUID(row.MessageID.Bytes).String()
		grouped[messageID] = append(
			grouped[messageID],
			attachmentFromDB(row),
		)
	}

	for i := range items {
		attachments := grouped[items[i].ID]
		if attachments == nil {
			attachments = []Attachment{}
		}

		items[i].Attachments = attachments
	}

	return nil
}

func attachmentsFromDB(rows []db.MessageAttachment) []Attachment {
	attachments := make([]Attachment, 0, len(rows))

	for _, row := range rows {
		attachments = append(attachments, attachmentFromDB(row))
	}

	return attachments
}

func attachmentFromDB(row db.MessageAttachment) Attachment {
	attachment := Attachment{
		ID:               uuid.UUID(row.ID.Bytes).String(),
		TenantID:         row.TenantID,
		ConversationID:   uuid.UUID(row.ConversationID.Bytes).String(),
		UploadedByUserID: row.UploadedByUserID,
		Filename:         row.Filename,
		ContentType:      row.ContentType,
		SizeBytes:        row.SizeBytes,
		ObjectKey:        row.ObjectKey,
		CreatedAt:        row.CreatedAt.Time,
	}

	if row.MessageID.Valid {
		value := uuid.UUID(row.MessageID.Bytes).String()
		attachment.MessageID = &value
	}

	if row.DeletedAt.Valid {
		value := row.DeletedAt.Time
		attachment.DeletedAt = &value
	}

	return attachment
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

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func textStringValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}

	return value.String
}

func (r *Repository) publishTx(
	ctx context.Context,
	tx pgx.Tx,
	event Event,
) error {
	if r.publisher == nil {
		return nil
	}

	if err := r.publisher.PublishTx(ctx, tx, event); err != nil {
		return fmt.Errorf("publish %s: %w", event.Kind, err)
	}

	return nil
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

	if len(row.MentionedUserIds) > 0 {
		message.Mentions = append(
			[]string(nil),
			row.MentionedUserIds...,
		)
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

// -------------------------------------------------------------------------
// Stream tokens
// -------------------------------------------------------------------------

type CreateStreamTokenParams struct {
	TokenHash []byte

	TenantID string

	UserID string

	TTL time.Duration
}

// CreateStreamToken stores the ticket hash and returns the expiry chosen by
// the database, so token lifetime does not depend on app/database clock skew.
func (r *Repository) CreateStreamToken(
	ctx context.Context,
	params CreateStreamTokenParams,
) (time.Time, error) {
	expiresAt, err := r.queries.CreateChatStreamToken(
		ctx,
		db.CreateChatStreamTokenParams{
			TokenHash: params.TokenHash,
			TenantID:  params.TenantID,
			UserID:    params.UserID,

			TtlMilliseconds: int32(params.TTL / time.Millisecond),
		},
	)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"create stream token: %w",
			err,
		)
	}

	return expiresAt.Time, nil
}

// ConsumeStreamToken deletes and returns a ticket atomically. A ticket can be
// used once, by any API instance, and expired tickets are never returned.
func (r *Repository) ConsumeStreamToken(
	ctx context.Context,
	tokenHash []byte,
) (StreamTokenIdentity, error) {
	row, err := r.queries.ConsumeChatStreamToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StreamTokenIdentity{}, ErrInvalidStreamToken
		}

		return StreamTokenIdentity{}, fmt.Errorf(
			"consume stream token: %w",
			err,
		)
	}

	return StreamTokenIdentity{
		TenantID: row.TenantID,

		UserID: row.UserID,
	}, nil
}

func (r *Repository) DeleteExpiredStreamTokens(
	ctx context.Context,
) (int64, error) {
	deleted, err := r.queries.DeleteExpiredChatStreamTokens(ctx)
	if err != nil {
		return 0, fmt.Errorf("delete expired stream tokens: %w", err)
	}

	return deleted, nil
}
