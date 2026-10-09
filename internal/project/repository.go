package project

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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
	ID              string
	TenantID        string
	Name            string
	Description     *string
	CreatedByUserID string
}

func (r *Repository) Create(
	ctx context.Context,
	params CreateParams,
) (Project, error) {

	projectID, err := uuid.Parse(params.ID)
	if err != nil {
		return Project{}, fmt.Errorf(
			"parse project id: %w",
			err,
		)
	}

	description := pgtype.Text{}

	if params.Description != nil {
		description = pgtype.Text{
			String: *params.Description,
			Valid:  true,
		}
	}

	row, err := r.queries.CreateProject(
		ctx,
		db.CreateProjectParams{
			ID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},
			TenantID:        params.TenantID,
			Name:            params.Name,
			Description:     description,
			CreatedByUserID: params.CreatedByUserID,
		},
	)
	if err != nil {
		return Project{}, fmt.Errorf(
			"create project: %w",
			err,
		)
	}

	return projectFromDB(row), nil
}

func projectFromDB(row db.Project) Project {
	project := Project{
		TenantID:        row.TenantID,
		Name:            row.Name,
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}

	if row.ID.Valid {
		project.ID = uuid.UUID(row.ID.Bytes).String()
	}

	if row.Description.Valid {
		description := row.Description.String
		project.Description = &description
	}

	if row.ArchivedAt.Valid {
		archivedAt := row.ArchivedAt.Time
		project.ArchivedAt = &archivedAt
	}

	return project
}

func (r *Repository) CreateWithDefaultStatuses(
	ctx context.Context,
	params CreateParams,
) (Project, error) {

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Project{}, fmt.Errorf(
			"begin transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	projectID, err := uuid.Parse(params.ID)
	if err != nil {
		return Project{}, fmt.Errorf(
			"parse project id: %w",
			err,
		)
	}

	description := pgtype.Text{}

	if params.Description != nil {
		description = pgtype.Text{
			String: *params.Description,
			Valid:  true,
		}
	}

	row, err := qtx.CreateProject(
		ctx,
		db.CreateProjectParams{
			ID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},
			TenantID:        params.TenantID,
			Name:            params.Name,
			Description:     description,
			CreatedByUserID: params.CreatedByUserID,
		},
	)
	if err != nil {
		return Project{}, fmt.Errorf(
			"create project: %w",
			err,
		)
	}

	err = qtx.AddProjectMember(
		ctx,
		db.AddProjectMemberParams{
			ProjectID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},
			TenantID: params.TenantID,
			UserID:   params.CreatedByUserID,
			Role:     "manager",
		},
	)
	if err != nil {
		return Project{}, fmt.Errorf(
			"add project creator as member: %w",
			err,
		)
	}

	defaultStatuses := []struct {
		Name     string
		Position int32
		Category string
	}{
		{
			Name:     "Todo",
			Position: 0,
			Category: "todo",
		},
		{
			Name:     "In Progress",
			Position: 1,
			Category: "in_progress",
		},
		{
			Name:     "Done",
			Position: 2,
			Category: "done",
		},
	}

	for _, status := range defaultStatuses {
		statusID := uuid.New()

		_, err := qtx.CreateProjectStatus(
			ctx,
			db.CreateProjectStatusParams{
				ID: pgtype.UUID{
					Bytes: statusID,
					Valid: true,
				},
				TenantID: params.TenantID,
				ProjectID: pgtype.UUID{
					Bytes: projectID,
					Valid: true,
				},
				Name:     status.Name,
				Position: status.Position,
				Category: status.Category,
			},
		)
		if err != nil {
			return Project{}, fmt.Errorf(
				"create default status %s: %w",
				status.Name,
				err,
			)
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return Project{}, fmt.Errorf(
			"commit transaction: %w",
			err,
		)
	}

	return projectFromDB(row), nil
}
