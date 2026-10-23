package member

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

func capacityFromDB(row db.MemberCapacity) Capacity {
	seconds := int64(row.DailyCapacitySeconds)
	return Capacity{UserID: row.UserID, Configured: true, DailyCapacitySeconds: &seconds, WorkingDays: append([]int16{}, row.WorkingDays...), Timezone: &row.Timezone}
}

func (r *Repository) getCapacity(ctx context.Context, tenantID, userID string) (Capacity, error) {
	row, err := r.queries.GetMemberCapacity(ctx, db.GetMemberCapacityParams{TenantID: tenantID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Capacity{UserID: userID}, nil
	}
	if err != nil {
		return Capacity{}, err
	}
	return capacityFromDB(row), nil
}

func (r *Repository) setCapacity(ctx context.Context, input CapacityInput) (Capacity, error) {
	row, err := r.queries.SetMemberCapacity(ctx, db.SetMemberCapacityParams{TenantID: input.Identity.TenantID, UserID: input.UserID, DailyCapacitySeconds: input.DailyCapacitySeconds, WorkingDays: input.WorkingDays, Timezone: input.Timezone})
	if errors.Is(err, pgx.ErrNoRows) {
		return Capacity{}, ErrNotFound
	}
	if err != nil {
		return Capacity{}, err
	}
	return capacityFromDB(row), nil
}

func (r *Repository) clearCapacity(ctx context.Context, tenantID, userID string) error {
	return r.queries.ClearMemberCapacity(ctx, db.ClearMemberCapacityParams{TenantID: tenantID, UserID: userID})
}

func (r *Repository) workload(ctx context.Context, input WorkloadInput, from, to time.Time, fullAccess bool) (WorkloadReport, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return WorkloadReport{}, err
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)
	params := db.ListWorkloadMembersParams{TenantID: input.Identity.TenantID, AfterUserID: input.Cursor, PageLimit: int32(input.Limit + 1)}
	if input.UserID != "" {
		params.UserID = pgtype.Text{String: input.UserID, Valid: true}
	}
	rows, err := queries.ListWorkloadMembers(ctx, params)
	if err != nil {
		return WorkloadReport{}, err
	}
	report := WorkloadReport{From: input.From, To: input.To, Scope: "visible_projects", Items: []WorkloadMember{}}
	if fullAccess {
		report.Scope = "workspace"
	}
	if len(rows) > input.Limit {
		rows = rows[:input.Limit]
		next := rows[len(rows)-1].UserID
		report.NextCursor = &next
	}
	ids := make([]string, 0, len(rows))
	byUser := make(map[string]int, len(rows))
	locations := make(map[string]*time.Location, len(rows))
	for _, row := range rows {
		capacity := Capacity{UserID: row.UserID}
		if row.DailyCapacitySeconds.Valid {
			seconds := int64(row.DailyCapacitySeconds.Int32)
			capacity.Configured = true
			capacity.DailyCapacitySeconds = &seconds
			capacity.WorkingDays = append([]int16{}, row.WorkingDays...)
			capacity.Timezone = &row.Timezone.String
		}
		item := initializeWorkload(row.UserID, row.FirstName, row.LastName, capacity, input.Timezone, from, to)
		zone, err := capacityLocation(item.Timezone)
		if err != nil {
			return WorkloadReport{}, err
		}
		ids = append(ids, row.UserID)
		byUser[row.UserID] = len(report.Items)
		locations[row.UserID] = zone
		report.Items = append(report.Items, item)
	}
	if len(ids) > 0 {
		tasks, err := queries.ListWorkloadTasks(ctx, db.ListWorkloadTasksParams{TenantID: input.Identity.TenantID, UserIds: ids, FullAccess: fullAccess, CallerUserID: input.Identity.UserID, Timezone: input.Timezone, ToDate: pgtype.Date{Time: to, Valid: true}})
		if err != nil {
			return WorkloadReport{}, err
		}
		for _, row := range tasks {
			item := workloadTask{RemainingSeconds: row.RemainingSeconds, Estimated: row.EstimateSeconds.Valid}
			if row.StartAt.Valid {
				item.StartAt = &row.StartAt.Time
			}
			if row.DueAt.Valid {
				item.DueAt = &row.DueAt.Time
			}
			userID := row.AssigneeUserID.String
			report.Items[byUser[userID]].allocate(item, from, to, locations[userID])
		}
	}
	for i := range report.Items {
		report.Items[i].finish()
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkloadReport{}, err
	}
	return report, nil
}
