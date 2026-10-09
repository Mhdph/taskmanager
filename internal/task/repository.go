package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/mhdph/taskmanager/internal/database/generated"
)

type Repository struct {
	queries *db.Queries
}

func NewRepository(
	queries *db.Queries,
) *Repository {
	return &Repository{
		queries: queries,
	}
}

type CreateParams struct {
	ID        string
	TenantID  string
	ProjectID string
	StatusID  string

	ParentTaskID *string

	Title       string
	Description *string

	Priority Priority

	AssigneeUserID *string

	CreatedByUserID string

	DueAt *time.Time
}

func (r *Repository) Create(
	ctx context.Context,
	params CreateParams,
) (Task, error) {
	taskID, err := uuid.Parse(params.ID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"parse generated task id: %w",
			err,
		)
	}

	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	statusID, err := uuid.Parse(params.StatusID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	var parentTaskID pgtype.UUID

	if params.ParentTaskID != nil {
		value, err := uuid.Parse(
			*params.ParentTaskID,
		)
		if err != nil {
			return Task{}, fmt.Errorf(
				"%w: %v",
				ErrInvalidID,
				err,
			)
		}

		parentTaskID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	var description pgtype.Text

	if params.Description != nil {
		description = pgtype.Text{
			String: *params.Description,
			Valid:  true,
		}
	}

	var assigneeUserID pgtype.Text

	if params.AssigneeUserID != nil {
		assigneeUserID = pgtype.Text{
			String: *params.AssigneeUserID,
			Valid:  true,
		}
	}

	var dueAt pgtype.Timestamptz

	if params.DueAt != nil {
		dueAt = pgtype.Timestamptz{
			Time:  *params.DueAt,
			Valid: true,
		}
	}

	row, err := r.queries.CreateTask(
		ctx,
		db.CreateTaskParams{
			ID: pgtype.UUID{
				Bytes: taskID,
				Valid: true,
			},

			TenantID: params.TenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},

			StatusID: pgtype.UUID{
				Bytes: statusID,
				Valid: true,
			},

			ParentTaskID: parentTaskID,

			Title:       params.Title,
			Description: description,

			Priority: string(params.Priority),

			AssigneeUserID: assigneeUserID,

			CreatedByUserID: params.CreatedByUserID,

			DueAt: dueAt,
		},
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"create task: %w",
			err,
		)
	}

	return taskFromDB(row), nil
}

func taskFromDB(
	row db.Task,
) Task {
	task := Task{
		TenantID:        row.TenantID,
		Title:           row.Title,
		Priority:        Priority(row.Priority),
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}

	if row.ID.Valid {
		task.ID = uuid.UUID(
			row.ID.Bytes,
		).String()
	}

	if row.ProjectID.Valid {
		task.ProjectID = uuid.UUID(
			row.ProjectID.Bytes,
		).String()
	}

	if row.StatusID.Valid {
		task.StatusID = uuid.UUID(
			row.StatusID.Bytes,
		).String()
	}

	if row.ParentTaskID.Valid {
		value := uuid.UUID(
			row.ParentTaskID.Bytes,
		).String()

		task.ParentTaskID = &value
	}

	if row.Description.Valid {
		value := row.Description.String
		task.Description = &value
	}

	if row.AssigneeUserID.Valid {
		value := row.AssigneeUserID.String
		task.AssigneeUserID = &value
	}

	if row.DueAt.Valid {
		value := row.DueAt.Time
		task.DueAt = &value
	}

	if row.CompletedAt.Valid {
		value := row.CompletedAt.Time
		task.CompletedAt = &value
	}

	if row.ArchivedAt.Valid {
		value := row.ArchivedAt.Time
		task.ArchivedAt = &value
	}

	return task
}

func (r *Repository) GetForProject(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
) (Task, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.GetTaskForProject(
		ctx,
		db.GetTaskForProjectParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			ID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrTaskNotFound
		}

		return Task{}, fmt.Errorf(
			"get task for project: %w",
			err,
		)
	}

	return taskFromDB(row), nil
}
