package team

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
	ID       string
	TenantID string

	Name string

	Icon  string
	Color string

	Description string

	CreatedByUserID string
}

func (r *Repository) Create(
	ctx context.Context,
	params CreateParams,
) (Team, error) {
	id, err := uuid.Parse(params.ID)
	if err != nil {
		return Team{}, fmt.Errorf(
			"parse team id: %w",
			err,
		)
	}

	row, err := r.queries.CreateTeam(
		ctx,
		db.CreateTeamParams{
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},

			TenantID: params.TenantID,

			Name: params.Name,

			Icon:  params.Icon,
			Color: params.Color,

			Description: params.Description,

			CreatedByUserID: params.CreatedByUserID,
		},
	)
	if err != nil {
		return Team{}, fmt.Errorf(
			"create team: %w",
			err,
		)
	}

	return teamFromDB(row), nil
}

func (r *Repository) Get(
	ctx context.Context,
	tenantID string,
	teamID string,
) (Team, error) {
	idUUID, err := parseTeamID(teamID)
	if err != nil {
		return Team{}, fmt.Errorf(
			"parse team id: %w",
			err,
		)
	}

	row, err := r.queries.GetTeam(
		ctx,
		db.GetTeamParams{
			TenantID: tenantID,

			ID: idUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Team{}, ErrTeamNotFound
		}

		return Team{}, fmt.Errorf(
			"get team: %w",
			err,
		)
	}

	return teamFromDB(row), nil
}

func (r *Repository) List(
	ctx context.Context,
	tenantID string,
) ([]Team, error) {
	rows, err := r.queries.ListTeams(
		ctx,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list teams: %w",
			err,
		)
	}

	teams := make(
		[]Team,
		0,
		len(rows),
	)

	for _, row := range rows {
		teams = append(
			teams,
			teamFromDB(row),
		)
	}

	return teams, nil
}

type UpdateParams struct {
	TenantID string
	ID       string

	Name        *string
	Icon        *string
	Color       *string
	Description *string
}

func (r *Repository) Update(
	ctx context.Context,
	params UpdateParams,
) (Team, error) {
	idUUID, err := parseTeamID(params.ID)
	if err != nil {
		return Team{}, fmt.Errorf(
			"parse team id: %w",
			err,
		)
	}

	row, err := r.queries.UpdateTeam(
		ctx,
		db.UpdateTeamParams{
			TenantID: params.TenantID,

			ID: idUUID,

			Name:        textPtr(params.Name),
			Icon:        textPtr(params.Icon),
			Color:       textPtr(params.Color),
			Description: textPtr(params.Description),
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Team{}, ErrTeamNotFound
		}

		return Team{}, fmt.Errorf(
			"update team: %w",
			err,
		)
	}

	return teamFromDB(row), nil
}

func (r *Repository) Delete(
	ctx context.Context,
	tenantID string,
	teamID string,
) error {
	idUUID, err := parseTeamID(teamID)
	if err != nil {
		return fmt.Errorf(
			"parse team id: %w",
			err,
		)
	}

	rows, err := r.queries.DeleteTeam(
		ctx,
		db.DeleteTeamParams{
			TenantID: tenantID,

			ID: idUUID,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"delete team: %w",
			err,
		)
	}

	if len(rows) == 0 {
		return ErrTeamNotFound
	}

	return nil
}

// ListMemberIDs returns the workspace member ids whose primary team is
// the given team.
func (r *Repository) ListMemberIDs(
	ctx context.Context,
	tenantID string,
	teamID string,
) ([]string, error) {
	idUUID, err := parseTeamID(teamID)
	if err != nil {
		return nil, fmt.Errorf(
			"parse team id: %w",
			err,
		)
	}

	ids, err := r.queries.ListTeamMemberIDs(
		ctx,
		db.ListTeamMemberIDsParams{
			TenantID: tenantID,

			TeamID: idUUID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list team members: %w",
			err,
		)
	}

	return ids, nil
}

// SetMembers replaces the team's membership in one transaction: members
// currently in the team are released and the requested ids become the
// team's members.
func (r *Repository) SetMembers(
	ctx context.Context,
	tenantID string,
	teamID string,
	userIDs []string,
) error {
	idUUID, err := parseTeamID(teamID)
	if err != nil {
		return fmt.Errorf(
			"parse team id: %w",
			err,
		)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf(
			"begin transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if err := qtx.ClearTeamMembership(
		ctx,
		db.ClearTeamMembershipParams{
			TenantID: tenantID,

			TeamID: idUUID,
		},
	); err != nil {
		return fmt.Errorf(
			"clear team membership: %w",
			err,
		)
	}

	if len(userIDs) > 0 {
		if err := qtx.SetTeamMembership(
			ctx,
			db.SetTeamMembershipParams{
				TeamID: idUUID,

				TenantID: tenantID,

				UserIds: userIDs,
			},
		); err != nil {
			return fmt.Errorf(
				"set team membership: %w",
				err,
			)
		}
	}

	return tx.Commit(ctx)
}

// ListMemberIDsIn returns the subset of userIDs that are workspace
// members.
func (r *Repository) ListMemberIDsIn(
	ctx context.Context,
	tenantID string,
	userIDs []string,
) ([]string, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	ids, err := r.queries.ListTenantMemberIDsIn(
		ctx,
		db.ListTenantMemberIDsInParams{
			TenantID: tenantID,

			UserIds: userIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve team members: %w",
			err,
		)
	}

	return ids, nil
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

func textPtr(
	value *string,
) pgtype.Text {
	if value == nil {
		return pgtype.Text{
			Valid: false,
		}
	}

	return pgtype.Text{
		String: *value,
		Valid:  true,
	}
}

func parseTeamID(
	teamID string,
) (pgtype.UUID, error) {
	id, err := uuid.Parse(teamID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf(
			"%w: %v",
			ErrTeamNotFound,
			err,
		)
	}

	return pgtype.UUID{
		Bytes: id,
		Valid: true,
	}, nil
}

func teamFromDB(row db.Team) Team {
	team := Team{
		TenantID: row.TenantID,

		Name: row.Name,

		Icon:  row.Icon,
		Color: row.Color,

		Description: row.Description,

		CreatedByUserID: row.CreatedByUserID,

		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}

	if row.ID.Valid {
		team.ID = uuid.UUID(row.ID.Bytes).String()
	}

	return team
}

// isTeamConflict reports the unique (tenant_id, name) violation.
func isTeamConflict(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "teams_tenant_id_name_key"
}
