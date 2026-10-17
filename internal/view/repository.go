package view

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	Name     string
	ViewType string

	Filters []Filter

	CreatedByUserID string
}

func (r *Repository) Create(
	ctx context.Context,
	params CreateParams,
) (SavedView, error) {
	id, err := uuid.Parse(params.ID)
	if err != nil {
		return SavedView{}, fmt.Errorf(
			"parse view id: %w",
			err,
		)
	}

	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return SavedView{}, fmt.Errorf(
			"parse view project id: %w",
			err,
		)
	}

	encoded, err := encodeFilters(params.Filters)
	if err != nil {
		return SavedView{}, err
	}

	row, err := r.queries.CreateSavedView(
		ctx,
		db.CreateSavedViewParams{
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},

			TenantID: params.TenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},

			Name:     params.Name,
			ViewType: params.ViewType,

			Filters: encoded,

			CreatedByUserID: params.CreatedByUserID,
		},
	)
	if err != nil {
		return SavedView{}, fmt.Errorf(
			"create saved view: %w",
			err,
		)
	}

	return savedViewFromDB(row), nil
}

func (r *Repository) Get(
	ctx context.Context,
	tenantID string,
	projectID string,
	viewID string,
) (SavedView, error) {
	projectUUID, viewUUID, err := parsePair(projectID, viewID)
	if err != nil {
		return SavedView{}, fmt.Errorf(
			"%w: %v",
			ErrViewInvalidFilter,
			err,
		)
	}

	row, err := r.queries.GetSavedView(
		ctx,
		db.GetSavedViewParams{
			TenantID: tenantID,

			ProjectID: projectUUID,

			ID: viewUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SavedView{}, ErrViewNotFound
		}

		return SavedView{}, fmt.Errorf(
			"get saved view: %w",
			err,
		)
	}

	return savedViewFromDB(row), nil
}

func (r *Repository) List(
	ctx context.Context,
	tenantID string,
	projectID string,
) ([]SavedView, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrViewInvalidFilter,
			err,
		)
	}

	rows, err := r.queries.ListSavedViewsForProject(
		ctx,
		db.ListSavedViewsForProjectParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list saved views: %w",
			err,
		)
	}

	views := make(
		[]SavedView,
		0,
		len(rows),
	)

	for _, row := range rows {
		views = append(
			views,
			savedViewFromDB(row),
		)
	}

	return views, nil
}

type UpdateParams struct {
	TenantID  string
	ProjectID string
	ID        string

	Name     *string
	ViewType *string
	Filters  *[]Filter
}

func (r *Repository) Update(
	ctx context.Context,
	params UpdateParams,
) (SavedView, error) {
	projectUUID, viewUUID, err := parsePair(params.ProjectID, params.ID)
	if err != nil {
		return SavedView{}, fmt.Errorf(
			"%w: %v",
			ErrViewInvalidFilter,
			err,
		)
	}

	var name pgtype.Text
	if params.Name != nil {
		name = pgtype.Text{
			String: *params.Name,
			Valid:  true,
		}
	}

	var viewType pgtype.Text
	if params.ViewType != nil {
		viewType = pgtype.Text{
			String: *params.ViewType,
			Valid:  true,
		}
	}

	var encoded []byte
	if params.Filters != nil {
		encoded, err = encodeFilters(*params.Filters)
		if err != nil {
			return SavedView{}, err
		}
	}

	row, err := r.queries.UpdateSavedView(
		ctx,
		db.UpdateSavedViewParams{
			Name:     name,
			ViewType: viewType,

			Filters: encoded,

			TenantID: params.TenantID,

			ProjectID: projectUUID,

			ID: viewUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SavedView{}, ErrViewNotFound
		}

		return SavedView{}, fmt.Errorf(
			"update saved view: %w",
			err,
		)
	}

	return savedViewFromDB(row), nil
}

func (r *Repository) Delete(
	ctx context.Context,
	tenantID string,
	projectID string,
	viewID string,
) error {
	projectUUID, viewUUID, err := parsePair(projectID, viewID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrViewInvalidFilter,
			err,
		)
	}

	affected, err := r.queries.DeleteSavedView(
		ctx,
		db.DeleteSavedViewParams{
			TenantID: tenantID,

			ProjectID: projectUUID,

			ID: viewUUID,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"delete saved view: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrViewNotFound
	}

	return nil
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

func parsePair(
	projectID string,
	viewID string,
) (pgtype.UUID, pgtype.UUID, error) {
	projectIDValue, err := uuid.Parse(projectID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf(
			"parse project id: %w",
			err,
		)
	}

	viewIDValue, err := uuid.Parse(viewID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf(
			"parse view id: %w",
			err,
		)
	}

	return pgtype.UUID{
		Bytes: projectIDValue,
		Valid: true,
	}, pgtype.UUID{
		Bytes: viewIDValue,
		Valid: true,
	}, nil
}

func encodeFilters(
	filters []Filter,
) ([]byte, error) {
	encoded, err := json.Marshal(filters)
	if err != nil {
		return nil, fmt.Errorf(
			"encode view filters: %w",
			err,
		)
	}

	return encoded, nil
}

func decodeFilters(
	raw []byte,
) []Filter {
	var filters []Filter

	if err := json.Unmarshal(raw, &filters); err != nil {
		// Stored values are written through validation; a malformed row
		// is treated as "no filters" rather than failing every read.
		return nil
	}

	return filters
}

func savedViewFromDB(row db.ProjectSavedView) SavedView {
	view := SavedView{
		TenantID: row.TenantID,

		Name:     row.Name,
		ViewType: row.ViewType,

		Filters: decodeFilters(row.Filters),

		CreatedByUserID: row.CreatedByUserID,

		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}

	if row.ID.Valid {
		view.ID = uuid.UUID(row.ID.Bytes).String()
	}

	if row.ProjectID.Valid {
		view.ProjectID = uuid.UUID(row.ProjectID.Bytes).String()
	}

	return view
}

// isViewConflict reports the unique (tenant, project, name) violation.
func isViewConflict(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "project_saved_views_tenant_id_project_id_name_key"
}
