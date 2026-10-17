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

	// Optional owning team; when set it must exist in the tenant.
	TeamID *string
}

type UpdateParams struct {
	TenantID  string
	ProjectID string

	Name *string

	UpdateDescription bool
	Description       *string

	// UpdateTeam replaces the owning team (nil clears the link) when
	// set; nil means leave the link untouched.
	UpdateTeam bool
	TeamID     *string
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

	// Same team validation as Update: the team must exist in the tenant
	// before it can own the project.
	var teamID pgtype.UUID

	if params.TeamID != nil {
		parsedTeam, err := uuid.Parse(*params.TeamID)
		if err != nil {
			return Project{}, fmt.Errorf(
				"%w: %v",
				ErrTeamNotFound,
				err,
			)
		}

		if _, err := r.queries.GetTeam(
			ctx,
			db.GetTeamParams{
				TenantID: params.TenantID,
				ID: pgtype.UUID{
					Bytes: parsedTeam,
					Valid: true,
				},
			},
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Project{}, ErrTeamNotFound
			}

			return Project{}, fmt.Errorf(
				"resolve project team: %w",
				err,
			)
		}

		teamID = pgtype.UUID{
			Bytes: parsedTeam,
			Valid: true,
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
			TeamID:          teamID,
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

	if row.TeamID.Valid {
		teamID := uuid.UUID(row.TeamID.Bytes).String()
		project.TeamID = &teamID
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

	// Team validation mirrors the non-transactional Create.
	var teamID pgtype.UUID

	if params.TeamID != nil {
		parsedTeam, err := uuid.Parse(*params.TeamID)
		if err != nil {
			return Project{}, fmt.Errorf(
				"%w: %v",
				ErrTeamNotFound,
				err,
			)
		}

		if _, err := qtx.GetTeam(
			ctx,
			db.GetTeamParams{
				TenantID: params.TenantID,
				ID: pgtype.UUID{
					Bytes: parsedTeam,
					Valid: true,
				},
			},
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Project{}, ErrTeamNotFound
			}

			return Project{}, fmt.Errorf(
				"resolve project team: %w",
				err,
			)
		}

		teamID = pgtype.UUID{
			Bytes: parsedTeam,
			Valid: true,
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
			TeamID:          teamID,
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
			Archived:        options.Archived,
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
			Archived:        options.Archived,
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

	// A non-nil team id must exist in the tenant; validation happens on the
	// shared queries facade (no cross-package dependency needed).
	var teamID pgtype.UUID

	if params.TeamID != nil {
		parsed, err := uuid.Parse(*params.TeamID)
		if err != nil {
			return Project{}, fmt.Errorf(
				"%w: %v",
				ErrTeamNotFound,
				err,
			)
		}

		if _, err := r.queries.GetTeam(
			ctx,
			db.GetTeamParams{
				TenantID: params.TenantID,

				ID: pgtype.UUID{
					Bytes: parsed,
					Valid: true,
				},
			},
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Project{}, ErrTeamNotFound
			}

			return Project{}, fmt.Errorf(
				"resolve project team: %w",
				err,
			)
		}

		teamID = pgtype.UUID{
			Bytes: parsed,
			Valid: true,
		}
	}

	row, err := r.queries.UpdateProject(
		ctx,
		db.UpdateProjectParams{
			Name: name,

			UpdateDescription: params.UpdateDescription,
			Description:       description,

			UpdateTeam: params.UpdateTeam,
			TeamID:     teamID,

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

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return ProjectMember{}, fmt.Errorf("begin member role transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.queries.WithTx(tx)
	if err := lockProjectForMembershipMutation(ctx, qtx, tenantID, id); err != nil {
		return ProjectMember{}, err
	}
	current, err := qtx.GetProjectMember(ctx, db.GetProjectMemberParams{
		TenantID:  tenantID,
		ProjectID: pgtype.UUID{Bytes: id, Valid: true},
		UserID:    userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectMember{}, ErrMemberNotFound
		}
		return ProjectMember{}, fmt.Errorf("get project member in transaction: %w", err)
	}
	if current.Role == string(MemberRoleManager) && role != MemberRoleManager {
		count, err := qtx.CountProjectManagers(ctx, db.CountProjectManagersParams{
			TenantID: tenantID, ProjectID: pgtype.UUID{Bytes: id, Valid: true},
		})
		if err != nil {
			return ProjectMember{}, fmt.Errorf("count project managers in transaction: %w", err)
		}
		if count <= 1 {
			return ProjectMember{}, ErrLastManager
		}
	}
	row, err := qtx.UpdateProjectMemberRole(
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
	if err := tx.Commit(ctx); err != nil {
		return ProjectMember{}, fmt.Errorf("commit member role transaction: %w", err)
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

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin member removal transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := r.queries.WithTx(tx)
	if err := lockProjectForMembershipMutation(ctx, qtx, tenantID, id); err != nil {
		return err
	}
	current, err := qtx.GetProjectMember(ctx, db.GetProjectMemberParams{
		TenantID: tenantID, ProjectID: pgtype.UUID{Bytes: id, Valid: true}, UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMemberNotFound
		}
		return fmt.Errorf("get project member in transaction: %w", err)
	}
	if current.Role == string(MemberRoleManager) {
		count, err := qtx.CountProjectManagers(ctx, db.CountProjectManagersParams{
			TenantID: tenantID, ProjectID: pgtype.UUID{Bytes: id, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("count project managers in transaction: %w", err)
		}
		if count <= 1 {
			return ErrLastManager
		}
	}
	affected, err := qtx.DeleteProjectMember(
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
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit member removal transaction: %w", err)
	}

	return nil
}

func lockProjectForMembershipMutation(ctx context.Context, qtx *db.Queries, tenantID string, projectID uuid.UUID) error {
	_, err := qtx.LockProjectForMembershipMutation(ctx, db.LockProjectForMembershipMutationParams{
		TenantID: tenantID, ProjectID: pgtype.UUID{Bytes: projectID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock project for membership mutation: %w", err)
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

	queries := r.queries
	var categoryTx pgx.Tx
	if params.Category != nil {
		categoryTx, err = r.db.Begin(ctx)
		if err != nil {
			return ProjectStatus{}, fmt.Errorf("begin status category transaction: %w", err)
		}
		defer categoryTx.Rollback(ctx)
		queries = r.queries.WithTx(categoryTx)
		current, err := queries.LockProjectStatusForUpdate(ctx, db.LockProjectStatusForUpdateParams{
			TenantID:  params.TenantID,
			ProjectID: pgtype.UUID{Bytes: projectID, Valid: true},
			ID:        pgtype.UUID{Bytes: statusID, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectStatus{}, ErrStatusNotFound
		}
		if err != nil {
			return ProjectStatus{}, fmt.Errorf("lock status category: %w", err)
		}
		if current.Category != string(*params.Category) {
			hasTasks, err := queries.ProjectStatusHasTasks(ctx, db.ProjectStatusHasTasksParams{
				TenantID:  params.TenantID,
				ProjectID: pgtype.UUID{Bytes: projectID, Valid: true},
				StatusID:  pgtype.UUID{Bytes: statusID, Valid: true},
			})
			if err != nil {
				return ProjectStatus{}, fmt.Errorf("check status usage: %w", err)
			}
			if hasTasks {
				return ProjectStatus{}, ErrStatusInUse
			}
		}
	}

	row, err := queries.UpdateProjectStatus(
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

	if categoryTx != nil {
		if err := categoryTx.Commit(ctx); err != nil {
			return ProjectStatus{}, fmt.Errorf("commit status category transaction: %w", err)
		}
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
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrStatusInUse
		}
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

func (r *Repository) ReorderStatuses(ctx context.Context, tenantID, projectID string, statusIDs []string) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidID, err)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin status reorder: %w", err)
	}
	defer tx.Rollback(ctx)
	var locked uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM projects WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectUUID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock project for status reorder: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM project_statuses WHERE tenant_id=$1 AND project_id=$2 ORDER BY position FOR UPDATE`, tenantID, projectUUID)
	if err != nil {
		return fmt.Errorf("lock project statuses: %w", err)
	}
	current := make(map[uuid.UUID]struct{})
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("read project statuses: %w", err)
		}
		current[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read project statuses: %w", err)
	}
	rows.Close()
	if len(statusIDs) != len(current) {
		return ErrInvalidStatusOrder
	}
	parsed := make([]uuid.UUID, 0, len(statusIDs))
	seen := make(map[uuid.UUID]struct{}, len(statusIDs))
	for _, value := range statusIDs {
		id, err := uuid.Parse(value)
		if err != nil {
			return ErrInvalidStatusOrder
		}
		if _, ok := current[id]; !ok {
			return ErrInvalidStatusOrder
		}
		if _, ok := seen[id]; ok {
			return ErrInvalidStatusOrder
		}
		seen[id] = struct{}{}
		parsed = append(parsed, id)
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS project_statuses_project_id_position_key DEFERRED`); err != nil {
		return fmt.Errorf("defer status position constraint: %w", err)
	}
	for position, id := range parsed {
		if _, err := tx.Exec(ctx, `UPDATE project_statuses SET position=$1 WHERE tenant_id=$2 AND project_id=$3 AND id=$4`, position, tenantID, projectUUID, id); err != nil {
			return fmt.Errorf("update status position: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit status reorder: %w", err)
	}
	return nil
}
func (r *Repository) GetStatus(
	ctx context.Context,
	tenantID string,
	projectID string,
	statusID string,
) (ProjectStatus, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	statusUUID, err := uuid.Parse(statusID)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.GetProjectStatus(
		ctx,
		db.GetProjectStatusParams{
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
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectStatus{}, ErrStatusNotFound
		}

		return ProjectStatus{}, fmt.Errorf(
			"get project status: %w",
			err,
		)
	}

	return projectStatusFromDB(row), nil
}
