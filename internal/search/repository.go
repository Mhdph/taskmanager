package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

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

// Members searches the tenant member directory by name, email or title.
func (r *Repository) Members(
	ctx context.Context,
	tenantID string,
	query string,
	limit int32,
) ([]Result, error) {
	rows, err := r.queries.SearchMembers(
		ctx,
		db.SearchMembersParams{
			TenantID: tenantID,

			SearchQuery: query,

			PageLimit: limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"search members: %w",
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
			Type: ResultTypePerson,

			ID: row.UserID,

			Title: strings.TrimSpace(
				row.FirstName + " " + row.LastName,
			),
		}

		if item.Title == "" {
			item.Title = row.Email
		}

		description := row.Email

		item.Description = &description

		result = append(
			result,
			item,
		)
	}

	return result, nil
}

// Comments searches comment bodies across visible, non-archived projects.
func (r *Repository) Comments(
	ctx context.Context,
	tenantID string,
	userID string,
	isAdmin bool,
	query string,
	limit int32,
) ([]Result, error) {
	rows, err := r.queries.SearchComments(
		ctx,
		db.SearchCommentsParams{
			TenantID: tenantID,

			UserID: userID,

			IsAdmin: isAdmin,

			SearchQuery: query,

			PageLimit: limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"search comments: %w",
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
			Type: ResultTypeComment,

			Title: strings.TrimSpace(row.Body),
		}

		if row.ID.Valid {
			item.ID = uuid.UUID(
				row.ID.Bytes,
			).String()
		}

		if row.ProjectID.Valid {
			projectID := uuid.UUID(
				row.ProjectID.Bytes,
			).String()

			item.ProjectID = &projectID
		}

		if row.TaskID.Valid {
			taskID := uuid.UUID(
				row.TaskID.Bytes,
			).String()

			item.TaskID = &taskID
		}

		result = append(
			result,
			item,
		)
	}

	return result, nil
}

// Attachments searches attachment filenames across visible, non-archived
// projects.
func (r *Repository) Files(
	ctx context.Context,
	tenantID string,
	userID string,
	isAdmin bool,
	query string,
	limit int32,
) ([]Result, error) {
	rows, err := r.queries.SearchAttachments(
		ctx,
		db.SearchAttachmentsParams{
			TenantID: tenantID,

			UserID: userID,

			IsAdmin: isAdmin,

			SearchQuery: query,

			PageLimit: limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"search attachments: %w",
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
			Type: ResultTypeFile,

			Title: row.Filename,
		}

		if row.ID.Valid {
			item.ID = uuid.UUID(
				row.ID.Bytes,
			).String()
		}

		if row.ProjectID.Valid {
			projectID := uuid.UUID(
				row.ProjectID.Bytes,
			).String()

			item.ProjectID = &projectID
		}

		if row.TaskID.Valid {
			taskID := uuid.UUID(
				row.TaskID.Bytes,
			).String()

			item.TaskID = &taskID
		}

		result = append(
			result,
			item,
		)
	}

	return result, nil
}
