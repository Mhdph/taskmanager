package comment

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

type CreateParams struct {
	ID        string
	TenantID  string
	ProjectID string
	TaskID    string

	AuthorUserID string

	Body string

	MentionedUserIDs []string
}

type ListOptions struct {
	Limit  int32
	Cursor *Cursor
}

func (r *Repository) Create(
	ctx context.Context,
	params CreateParams,
) (Comment, error) {
	commentID, err := uuid.Parse(
		params.ID,
	)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"parse comment id: %w",
			err,
		)
	}

	projectID, err := uuid.Parse(
		params.ProjectID,
	)
	if err != nil {
		return Comment{},
			fmt.Errorf(
				"parse project id: %w",
				err,
			)
	}

	taskID, err := uuid.Parse(
		params.TaskID,
	)
	if err != nil {
		return Comment{},
			fmt.Errorf(
				"parse task id: %w",
				err,
			)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"begin transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	row, err := qtx.CreateTaskComment(
		ctx,
		db.CreateTaskCommentParams{
			ID: pgtype.UUID{
				Bytes: commentID,
				Valid: true,
			},

			TenantID: params.TenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskID,
				Valid: true,
			},

			AuthorUserID: params.AuthorUserID,

			Body: params.Body,
		},
	)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"create comment: %w",
			err,
		)
	}

	for _, userID := range params.MentionedUserIDs {

		err := qtx.AddTaskCommentMention(
			ctx,
			db.AddTaskCommentMentionParams{
				CommentID: pgtype.UUID{
					Bytes: commentID,
					Valid: true,
				},

				MentionedUserID: userID,
			},
		)
		if err != nil {
			return Comment{},
				fmt.Errorf(
					"add mention: %w",
					err,
				)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Comment{}, fmt.Errorf(
			"commit comment: %w",
			err,
		)
	}

	result := commentFromDB(row)

	result.MentionedUserIDs =
		params.MentionedUserIDs

	return result, nil
}

func (r *Repository) ListForTask(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	options ListOptions,
) ([]Comment, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid project id: %v", ErrInvalidID, err)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid task id: %v", ErrInvalidID, err)
	}

	var cursorCreatedAt pgtype.Timestamptz
	var cursorID pgtype.UUID
	if options.Cursor != nil {
		cursorCreatedAt = pgtype.Timestamptz{
			Time:  options.Cursor.CreatedAt,
			Valid: true,
		}

		value, err := uuid.Parse(options.Cursor.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid cursor id: %v", ErrInvalidID, err)
		}
		cursorID = pgtype.UUID{Bytes: value, Valid: true}
	}

	rows, err := r.queries.ListTaskComments(
		ctx,
		db.ListTaskCommentsParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},
			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
			CursorCreatedAt: cursorCreatedAt,
			CursorID:        cursorID,
			PageLimit:       options.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list task comments: %w", err)
	}

	result := make([]Comment, 0, len(rows))
	for _, row := range rows {
		item := commentFromDB(row)
		commentID, err := uuid.Parse(item.ID)
		if err != nil {
			return nil, fmt.Errorf("parse comment id: %w", err)
		}

		mentions, err := r.queries.ListTaskCommentMentions(
			ctx,
			pgtype.UUID{Bytes: commentID, Valid: true},
		)
		if err != nil {
			return nil, fmt.Errorf("list comment mentions: %w", err)
		}
		item.MentionedUserIDs = mentions
		result = append(result, item)
	}

	return result, nil
}

func (r *Repository) Delete(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	commentID string,
) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf("%w: invalid project id: %v", ErrInvalidID, err)
	}
	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return fmt.Errorf("%w: invalid task id: %v", ErrInvalidID, err)
	}
	commentUUID, err := uuid.Parse(commentID)
	if err != nil {
		return fmt.Errorf("%w: invalid comment id: %v", ErrInvalidID, err)
	}

	affected, err := r.queries.DeleteTaskComment(
		ctx,
		db.DeleteTaskCommentParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},
			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
			ID: pgtype.UUID{
				Bytes: commentUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("delete task comment: %w", err)
	}
	if affected == 0 {
		return ErrCommentNotFound
	}
	return nil
}

func commentFromDB(
	row db.TaskComment,
) Comment {
	comment := Comment{
		TenantID: row.TenantID,

		AuthorUserID: row.AuthorUserID,

		Body: row.Body,

		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}

	if row.ID.Valid {
		comment.ID = uuid.UUID(
			row.ID.Bytes,
		).String()
	}

	if row.ProjectID.Valid {
		comment.ProjectID =
			uuid.UUID(
				row.ProjectID.Bytes,
			).String()
	}

	if row.TaskID.Valid {
		comment.TaskID = uuid.UUID(
			row.TaskID.Bytes,
		).String()
	}

	return comment
}

func (r *Repository) Get(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	commentID string,
) (Comment, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	commentUUID, err := uuid.Parse(commentID)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"%w: invalid comment id: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.GetTaskComment(
		ctx,
		db.GetTaskCommentParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},

			ID: pgtype.UUID{
				Bytes: commentUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Comment{},
				ErrCommentNotFound
		}

		return Comment{}, fmt.Errorf(
			"get task comment: %w",
			err,
		)
	}

	item := commentFromDB(row)

	mentions, err := r.queries.ListTaskCommentMentions(
		ctx,
		row.ID,
	)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"list comment mentions: %w",
			err,
		)
	}

	item.MentionedUserIDs = mentions

	return item, nil
}
