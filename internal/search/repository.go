package search

import (
	"context"
	"fmt"
	"uuid"

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
func (r *Repository) Projects(
	ctx context.Context,
	tenantID string,
	userID string,
	isAdmin bool,
	query string,
	limit int32,
) ([]Result, error) {
	rows, err := r.queries.SearchProjects(
		ctx,
		db.SearchProjectsParams{
			TenantID: tenantID,

			UserID: userID,

			IsAdmin: isAdmin,

			SearchQuery: query,

			PageLimit: limit,
		},
	)

	if err != nil {
		return nil, fmt.Errorf(
			"search projects: %w",
			err,
		)
	}

	result := make(
		[]Result,
		0,
		len(rows),
	)

	for _, row := range rows {
		item := Result{
			Type: ResultTypeProject,

			Title: row.Name,
		}

		if row.ID.Valid {
			item.ID = uuid.UUID(
				row.ID.Bytes,
			).String()
		}

		if row.Description.Valid {
			value :=
				row.Description.String

			item.Description =
				&value
		}

		result = append(
			result,
			item,
		)
	}

	return result, nil
}
func (r *Repository) Tasks(
	ctx context.Context,
	tenantID string,
	userID string,
	isAdmin bool,
	query string,
	limit int32,
) ([]Result, error) {
	rows, err := r.queries.SearchTasks(
		ctx,
		db.SearchTasksParams{
			TenantID: tenantID,

			UserID: userID,

			IsAdmin: isAdmin,

			SearchQuery: query,

			PageLimit: limit,
		},
	)

	if err != nil {
		return nil, fmt.Errorf(
			"search tasks: %w",
			err,
		)
	}

	result := make(
		[]Result,
		0,
		len(rows),
	)

	for _, row := range rows {
		item := Result{
			Type: ResultTypeTask,

			Title: row.Title,
		}

		if row.ID.Valid {
			item.ID = uuid.UUID(
				row.ID.Bytes,
			).String()
		}

		if row.ProjectID.Valid {
			value := uuid.UUID(
				row.ProjectID.Bytes,
			).String()

			item.ProjectID =
				&value
		}

		if row.Description.Valid {
			value :=
				row.Description.String

			item.Description =
				&value
		}

		result = append(
			result,
			item,
		)
	}

	return result, nil
}
