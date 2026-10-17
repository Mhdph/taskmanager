package favorite

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

// Repository persists per-user favorites over tasks and projects.
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

func (r *Repository) AddTask(
	ctx context.Context,
	tenantID string,
	userID string,
	projectID string,
	taskID string,
) error {
	projectUUID, taskUUID, err := parseIDs(
		projectID,
		taskID,
	)
	if err != nil {
		return err
	}

	err = r.queries.AddFavoriteTask(
		ctx,
		db.AddFavoriteTaskParams{
			TenantID: tenantID,
			UserID:   userID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"add favorite task: %w",
			err,
		)
	}

	return nil
}

func (r *Repository) RemoveTask(
	ctx context.Context,
	tenantID string,
	userID string,
	taskID string,
) error {
	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.RemoveFavoriteTask(
		ctx,
		db.RemoveFavoriteTaskParams{
			TenantID: tenantID,
			UserID:   userID,

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"remove favorite task: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *Repository) ListTaskIDs(
	ctx context.Context,
	tenantID string,
	userID string,
) ([]string, error) {
	rows, err := r.queries.ListFavoriteTaskIDs(
		ctx,
		db.ListFavoriteTaskIDsParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list favorite tasks: %w",
			err,
		)
	}

	ids := make(
		[]string,
		0,
		len(rows),
	)

	for _, row := range rows {
		ids = append(
			ids,
			uuid.UUID(row.Bytes).String(),
		)
	}

	return ids, nil
}

func (r *Repository) AddProject(
	ctx context.Context,
	tenantID string,
	userID string,
	projectID string,
) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	err = r.queries.AddFavoriteProject(
		ctx,
		db.AddFavoriteProjectParams{
			TenantID: tenantID,
			UserID:   userID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"add favorite project: %w",
			err,
		)
	}

	return nil
}

func (r *Repository) RemoveProject(
	ctx context.Context,
	tenantID string,
	userID string,
	projectID string,
) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.RemoveFavoriteProject(
		ctx,
		db.RemoveFavoriteProjectParams{
			TenantID: tenantID,
			UserID:   userID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"remove favorite project: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *Repository) ListProjectIDs(
	ctx context.Context,
	tenantID string,
	userID string,
) ([]string, error) {
	rows, err := r.queries.ListFavoriteProjectIDs(
		ctx,
		db.ListFavoriteProjectIDsParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list favorite projects: %w",
			err,
		)
	}

	ids := make(
		[]string,
		0,
		len(rows),
	)

	for _, row := range rows {
		ids = append(
			ids,
			uuid.UUID(row.Bytes).String(),
		)
	}

	return ids, nil
}

func parseIDs(
	first string,
	second string,
) (uuid.UUID, uuid.UUID, error) {
	firstUUID, err := uuid.Parse(first)
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	secondUUID, err := uuid.Parse(second)
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	return firstUUID, secondUUID, nil
}
