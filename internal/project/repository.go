package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

type UpdateParams struct {
	TenantID  string
	ProjectID string

	Name *string

	UpdateDescription bool
	Description       *string
}

type AddMemberParams struct {
	TenantID  string
	ProjectID string
	UserID    string
	Role      MemberRole
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

	_, err = qtx.AddProjectMember(
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

func (r *Repository) ListForTenant(
	ctx context.Context,
	tenantID string,
	options ListOptions,
) ([]Project, error) {
	var cursorCreatedAt pgtype.Timestamptz
	var cursorID pgtype.UUID

	if options.Cursor != nil {
		cursorCreatedAt = pgtype.Timestamptz{
			Time:  options.Cursor.CreatedAt,
			Valid: true,
		}

		id, err := uuid.Parse(options.Cursor.ID)
		if err != nil {
			return nil, fmt.Errorf(
				"parse cursor project id: %w",
				err,
			)
		}

		cursorID = pgtype.UUID{
			Bytes: id,
			Valid: true,
		}
	}

	rows, err := r.queries.ListProjectsForTenant(
		ctx,
		db.ListProjectsForTenantParams{
			TenantID:        tenantID,
			CursorCreatedAt: cursorCreatedAt,
			CursorID:        cursorID,
			PageLimit:       options.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list projects for tenant: %w",
			err,
		)
	}

	projects := make(
		[]Project,
		0,
		len(rows),
	)

	for _, row := range rows {
		projects = append(
			projects,
			projectFromDB(row),
		)
	}

	return projects, nil
}

func (r *Repository) ListForUser(
	ctx context.Context,
	tenantID string,
	userID string,
	options ListOptions,
) ([]Project, error) {
	var cursorCreatedAt pgtype.Timestamptz
	var cursorID pgtype.UUID

	if options.Cursor != nil {
		cursorCreatedAt = pgtype.Timestamptz{
			Time:  options.Cursor.CreatedAt,
			Valid: true,
		}

		id, err := uuid.Parse(options.Cursor.ID)
		if err != nil {
			return nil, fmt.Errorf(
				"parse cursor project id: %w",
				err,
			)
		}

		cursorID = pgtype.UUID{
			Bytes: id,
			Valid: true,
		}
	}

	rows, err := r.queries.ListProjectsForUser(
		ctx,
		db.ListProjectsForUserParams{
			TenantID:        tenantID,
			UserID:          userID,
			CursorCreatedAt: cursorCreatedAt,
			CursorID:        cursorID,
			PageLimit:       options.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list projects for user: %w",
			err,
		)
	}

	projects := make(
		[]Project,
		0,
		len(rows),
	)

	for _, row := range rows {
		projects = append(
			projects,
			projectFromDB(row),
		)
	}

	return projects, nil
}

func (r *Repository) GetForTenant(
	ctx context.Context,
	tenantID string,
	projectID string,
) (Project, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return Project{}, fmt.Errorf(
			"parse project id: %w",
			ErrInvalidID,
		)
	}

	row, err := r.queries.GetProjectForTenant(
		ctx,
		db.GetProjectForTenantParams{
			TenantID: tenantID,
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		return Project{}, wrapGetError(
			"get project for tenant",
			err,
		)
	}

	return projectFromDB(row), nil
}

func (r *Repository) GetForUser(
	ctx context.Context,
	tenantID string,
	userID string,
	projectID string,
) (Project, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return Project{}, fmt.Errorf(
			"parse project id: %w",
			ErrInvalidID,
		)
	}

	row, err := r.queries.GetProjectForUser(
		ctx,
		db.GetProjectForUserParams{
			TenantID: tenantID,
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
			UserID: userID,
		},
	)
	if err != nil {
		return Project{}, wrapGetError(
			"get project for user",
			err,
		)
	}

	return projectFromDB(row), nil
}

func wrapGetError(
	action string,
	err error,
) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf(
			"%s: %w",
			action,
			ErrNotFound,
		)
	}

	return fmt.Errorf(
		"%s: %w",
		action,
		err,
	)
}
func (r *Repository) GetMemberRole(
	ctx context.Context,
	tenantID string,
	projectID string,
	userID string,
) (MemberRole, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return "", fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	member, err := r.queries.GetProjectMember(
		ctx,
		db.GetProjectMemberParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
			UserID: userID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}

		return "", fmt.Errorf(
			"get project member: %w",
			err,
		)
	}

	return MemberRole(member.Role), nil
}

func (r *Repository) Update(
	ctx context.Context,
	params UpdateParams,
) (Project, error) {
	id, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return Project{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
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

	var description pgtype.Text

	if params.Description != nil {
		description = pgtype.Text{
			String: *params.Description,
			Valid:  true,
		}
	}

	row, err := r.queries.UpdateProject(
		ctx,
		db.UpdateProjectParams{
			Name: name,

			UpdateDescription: params.UpdateDescription,
			Description:       description,

			TenantID: params.TenantID,

			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}

		return Project{}, fmt.Errorf(
			"update project: %w",
			err,
		)
	}

	return projectFromDB(row), nil
}

func (r *Repository) Archive(
	ctx context.Context,
	tenantID string,
	projectID string,
) error {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.ArchiveProject(
		ctx,
		db.ArchiveProjectParams{
			TenantID: tenantID,
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"archive project: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *Repository) AddMember(
	ctx context.Context,
	params AddMemberParams,
) (ProjectMember, error) {
	projectID, err := uuid.Parse(
		params.ProjectID,
	)
	if err != nil {
		return ProjectMember{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.AddProjectMember(
		ctx,
		db.AddProjectMemberParams{
			ProjectID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},
			TenantID: params.TenantID,
			UserID:   params.UserID,
			Role:     string(params.Role),
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" {
			return ProjectMember{}, ErrMemberExists
		}

		return ProjectMember{}, fmt.Errorf(
			"add project member: %w",
			err,
		)
	}

	return projectMemberFromDB(row), nil
}

func projectMemberFromDB(
	row db.ProjectMember,
) ProjectMember {
	member := ProjectMember{
		TenantID:  row.TenantID,
		UserID:    row.UserID,
		Role:      MemberRole(row.Role),
		CreatedAt: row.CreatedAt.Time,
	}

	if row.ProjectID.Valid {
		member.ProjectID = uuid.UUID(
			row.ProjectID.Bytes,
		).String()
	}

	return member
}
func (r *Repository) ListMembers(
	ctx context.Context,
	tenantID string,
	projectID string,
) ([]ProjectMember, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	rows, err := r.queries.ListProjectMembers(
		ctx,
		db.ListProjectMembersParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list project members: %w",
			err,
		)
	}

	members := make(
		[]ProjectMember,
		0,
		len(rows),
	)

	for _, row := range rows {
		members = append(
			members,
			projectMemberFromDB(row),
		)
	}

	return members, nil
}
func (r *Repository) UpdateMemberRole(
	ctx context.Context,
	tenantID string,
	projectID string,
	userID string,
	role MemberRole,
) (ProjectMember, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return ProjectMember{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.UpdateProjectMemberRole(
		ctx,
		db.UpdateProjectMemberRoleParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
			UserID: userID,
			Role:   string(role),
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectMember{}, ErrMemberNotFound
		}

		return ProjectMember{}, fmt.Errorf(
			"update project member role: %w",
			err,
		)
	}

	return projectMemberFromDB(row), nil
}
func (r *Repository) DeleteMember(
	ctx context.Context,
	tenantID string,
	projectID string,
	userID string,
) error {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.DeleteProjectMember(
		ctx,
		db.DeleteProjectMemberParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
			UserID: userID,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"delete project member: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrMemberNotFound
	}

	return nil
}
func (r *Repository) CountManagers(
	ctx context.Context,
	tenantID string,
	projectID string,
) (int64, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	count, err := r.queries.CountProjectManagers(
		ctx,
		db.CountProjectManagersParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		return 0, fmt.Errorf(
			"count project managers: %w",
			err,
		)
	}

	return count, nil
}

func projectStatusFromDB(
	row db.ProjectStatus,
) ProjectStatus {
	status := ProjectStatus{
		TenantID:  row.TenantID,
		Name:      row.Name,
		Position:  row.Position,
		Category:  StatusCategory(row.Category),
		CreatedAt: row.CreatedAt.Time,
	}

	if row.ID.Valid {
		status.ID = uuid.UUID(
			row.ID.Bytes,
		).String()
	}

	if row.ProjectID.Valid {
		status.ProjectID = uuid.UUID(
			row.ProjectID.Bytes,
		).String()
	}

	return status
}

func (r *Repository) ListStatuses(
	ctx context.Context,
	tenantID string,
	projectID string,
) ([]ProjectStatus, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	rows, err := r.queries.ListProjectStatuses(
		ctx,
		db.ListProjectStatusesParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list project statuses: %w",
			err,
		)
	}

	statuses := make(
		[]ProjectStatus,
		0,
		len(rows),
	)

	for _, row := range rows {
		statuses = append(
			statuses,
			projectStatusFromDB(row),
		)
	}

	return statuses, nil
}

type CreateStatusParams struct {
	ID        string
	TenantID  string
	ProjectID string

	Name     string
	Position int32
	Category StatusCategory
}

func (r *Repository) CreateStatus(
	ctx context.Context,
	params CreateStatusParams,
) (ProjectStatus, error) {
	statusID, err := uuid.Parse(params.ID)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"parse status id: %w",
			err,
		)
	}

	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.CreateProjectStatus(
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
			Name:     params.Name,
			Position: params.Position,
			Category: string(params.Category),
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" {

			switch pgErr.ConstraintName {
			case "project_statuses_project_id_name_key":
				return ProjectStatus{}, ErrStatusAlreadyExists

			case "project_statuses_project_id_position_key":
				return ProjectStatus{}, ErrStatusPositionConflict
			}
		}

		return ProjectStatus{}, fmt.Errorf(
			"create project status: %w",
			err,
		)
	}

	return projectStatusFromDB(row), nil
}

type UpdateStatusParams struct {
	TenantID  string
	ProjectID string
	StatusID  string

	Name     *string
	Position *int32
	Category *StatusCategory
}

func (r *Repository) UpdateStatus(
	ctx context.Context,
	params UpdateStatusParams,
) (ProjectStatus, error) {
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	statusID, err := uuid.Parse(params.StatusID)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
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

	var position pgtype.Int4
	if params.Position != nil {
		position = pgtype.Int4{
			Int32: *params.Position,
			Valid: true,
		}
	}

	var category pgtype.Text
	if params.Category != nil {
		category = pgtype.Text{
			String: string(*params.Category),
			Valid:  true,
		}
	}

	row, err := r.queries.UpdateProjectStatus(
		ctx,
		db.UpdateProjectStatusParams{
			Name:     name,
			Position: position,
			Category: category,

			TenantID: params.TenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},

			ID: pgtype.UUID{
				Bytes: statusID,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectStatus{}, ErrStatusNotFound
		}

		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" {

			switch pgErr.ConstraintName {
			case "project_statuses_project_id_name_key":
				return ProjectStatus{}, ErrStatusAlreadyExists

			case "project_statuses_project_id_position_key":
				return ProjectStatus{}, ErrStatusPositionConflict
			}
		}

		return ProjectStatus{}, fmt.Errorf(
			"update project status: %w",
			err,
		)
	}

	return projectStatusFromDB(row), nil
}

func (r *Repository) DeleteStatus(
	ctx context.Context,
	tenantID string,
	projectID string,
	statusID string,
) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	statusUUID, err := uuid.Parse(statusID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.DeleteProjectStatus(
		ctx,
		db.DeleteProjectStatusParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			ID: pgtype.UUID{
				Bytes: statusUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"delete project status: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrStatusNotFound
	}

	return nil
}
