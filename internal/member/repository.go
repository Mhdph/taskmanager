package member

import (
	"context"
	"errors"
	"fmt"
	"time"

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

type SyncParams struct {
	TenantID  string
	UserID    string
	Email     string
	FirstName string
	LastName  string
}

func (r *Repository) Sync(
	ctx context.Context,
	params SyncParams,
) error {
	err := r.queries.SyncTenantMember(
		ctx,
		db.SyncTenantMemberParams{
			TenantID:  params.TenantID,
			UserID:    params.UserID,
			Email:     params.Email,
			FirstName: params.FirstName,
			LastName:  params.LastName,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"sync tenant member: %w",
			err,
		)
	}

	return nil
}

func (r *Repository) Get(
	ctx context.Context,
	tenantID string,
	userID string,
) (Member, error) {
	row, err := r.queries.GetTenantMember(
		ctx,
		db.GetTenantMemberParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, ErrNotFound
		}

		return Member{}, fmt.Errorf(
			"get tenant member: %w",
			err,
		)
	}

	return memberFromDB(row), nil
}

func (r *Repository) List(
	ctx context.Context,
	tenantID string,
	options ListOptions,
) ([]Member, error) {
	var role pgtype.Text

	if options.Role != nil {
		role = pgtype.Text{
			String: string(*options.Role),
			Valid:  true,
		}
	}

	var status pgtype.Text

	if options.Status != nil {
		status = pgtype.Text{
			String: string(*options.Status),
			Valid:  true,
		}
	}

	var search pgtype.Text

	if options.Search != nil {
		search = pgtype.Text{
			String: *options.Search,
			Valid:  true,
		}
	}

	rows, err := r.queries.ListTenantMembers(
		ctx,
		db.ListTenantMembersParams{
			TenantID: tenantID,
			Limit:    options.Limit,
			Role:     role,
			Status:   status,
			Search:   search,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list tenant members: %w",
			err,
		)
	}

	members := make(
		[]Member,
		0,
		len(rows),
	)

	for _, row := range rows {
		members = append(
			members,
			memberFromDB(row),
		)
	}

	return members, nil
}

func (r *Repository) UpdateRole(
	ctx context.Context,
	tenantID string,
	userID string,
	role Role,
) (Member, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Member{}, fmt.Errorf(
			"begin member role transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if err := qtx.LockTenantForMembershipMutation(
		ctx,
		tenantID,
	); err != nil {
		return Member{}, fmt.Errorf(
			"lock tenant for membership mutation: %w",
			err,
		)
	}

	current, err := qtx.GetTenantMember(
		ctx,
		db.GetTenantMemberParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, ErrNotFound
		}

		return Member{}, fmt.Errorf(
			"get tenant member in transaction: %w",
			err,
		)
	}

	if err := guardLastOwner(
		current.Role,
		string(role),
		func() (int64, error) {
			return qtx.CountTenantOwners(ctx, tenantID)
		},
	); err != nil {
		return Member{}, err
	}

	if _, err := qtx.UpdateTenantMemberRole(
		ctx,
		db.UpdateTenantMemberRoleParams{
			TenantID: tenantID,
			UserID:   userID,
			Role:     string(role),
		},
	); err != nil {
		return Member{}, fmt.Errorf(
			"update tenant member role: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return Member{}, fmt.Errorf(
			"commit member role transaction: %w",
			err,
		)
	}

	updated := memberFromDB(current)
	updated.Role = role
	updated.UpdatedAt = time.Now().UTC()

	return updated, nil
}

func (r *Repository) UpdateTitle(
	ctx context.Context,
	tenantID string,
	userID string,
	title string,
) (Member, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Member{}, fmt.Errorf(
			"begin member title transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if err := qtx.LockTenantForMembershipMutation(
		ctx,
		tenantID,
	); err != nil {
		return Member{}, fmt.Errorf(
			"lock tenant for membership mutation: %w",
			err,
		)
	}

	current, err := qtx.GetTenantMember(
		ctx,
		db.GetTenantMemberParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, ErrNotFound
		}

		return Member{}, fmt.Errorf(
			"get tenant member in transaction: %w",
			err,
		)
	}

	if _, err := qtx.UpdateTenantMemberTitle(
		ctx,
		db.UpdateTenantMemberTitleParams{
			TenantID: tenantID,
			UserID:   userID,
			Title:    title,
		},
	); err != nil {
		return Member{}, fmt.Errorf(
			"update tenant member title: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return Member{}, fmt.Errorf(
			"commit member title transaction: %w",
			err,
		)
	}

	updated := memberFromDB(current)
	updated.Title = title
	updated.UpdatedAt = time.Now().UTC()

	return updated, nil
}

func (r *Repository) Delete(
	ctx context.Context,
	tenantID string,
	userID string,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf(
			"begin member removal transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if err := qtx.LockTenantForMembershipMutation(
		ctx,
		tenantID,
	); err != nil {
		return fmt.Errorf(
			"lock tenant for membership mutation: %w",
			err,
		)
	}

	current, err := qtx.GetTenantMember(
		ctx,
		db.GetTenantMemberParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}

		return fmt.Errorf(
			"get tenant member in transaction: %w",
			err,
		)
	}

	if err := guardLastOwner(
		current.Role,
		"",
		func() (int64, error) {
			return qtx.CountTenantOwners(ctx, tenantID)
		},
	); err != nil {
		return err
	}

	affected, err := qtx.DeleteTenantMember(
		ctx,
		db.DeleteTenantMemberParams{
			TenantID: tenantID,
			UserID:   userID,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"delete tenant member: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit member removal transaction: %w",
			err,
		)
	}

	return nil
}

func memberFromDB(
	row db.TenantMember,
) Member {
	return Member{
		TenantID:  row.TenantID,
		UserID:    row.UserID,
		Email:     row.Email,
		FirstName: row.FirstName,
		LastName:  row.LastName,
		Role:      Role(row.Role),
		Title:     row.Title,
		Status:    Status(row.Status),

		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

// guardLastOwner rejects demoting or removing the last workspace owner.
// nextRole is "" when the member is being removed entirely.
func guardLastOwner(
	currentRole string,
	nextRole string,
	countOwners func() (int64, error),
) error {
	if currentRole != string(RoleOwner) {
		return nil
	}

	if nextRole == string(RoleOwner) {
		return nil
	}

	count, err := countOwners()
	if err != nil {
		return fmt.Errorf(
			"count tenant owners: %w",
			err,
		)
	}

	if count <= 1 {
		return ErrLastOwner
	}

	return nil
}
