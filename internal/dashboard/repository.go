package dashboard

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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

func (r *Repository) GetProjectDashboard(
	ctx context.Context,
	tenantID string,
	projectID string,
) (Dashboard, error) {
	projectUUID, err := uuid.Parse(
		projectID,
	)
	if err != nil {
		return Dashboard{}, fmt.Errorf(
			"invalid project id: %w",
			err,
		)
	}

	pgProjectID := pgtype.UUID{
		Bytes: projectUUID,
		Valid: true,
	}

	summaryRow, err :=
		r.queries.GetProjectDashboardSummary(
			ctx,
			db.GetProjectDashboardSummaryParams{
				TenantID: tenantID,

				ProjectID: pgProjectID,
			},
		)

	if err != nil {
		return Dashboard{}, fmt.Errorf(
			"get dashboard summary: %w",
			err,
		)
	}

	trackedSeconds, err :=
		r.queries.GetProjectTrackedSeconds(
			ctx,
			db.GetProjectTrackedSecondsParams{
				TenantID: tenantID,

				ProjectID: pgProjectID,
			},
		)

	if err != nil {
		return Dashboard{}, fmt.Errorf(
			"get tracked time: %w",
			err,
		)
	}

	statusRows, err :=
		r.queries.GetProjectDashboardByStatus(
			ctx,
			db.GetProjectDashboardByStatusParams{
				TenantID: tenantID,

				ProjectID: pgProjectID,
			},
		)

	if err != nil {
		return Dashboard{}, fmt.Errorf(
			"get dashboard statuses: %w",
			err,
		)
	}

	priorityRows, err :=
		r.queries.GetProjectDashboardByPriority(
			ctx,
			db.GetProjectDashboardByPriorityParams{
				TenantID: tenantID,

				ProjectID: pgProjectID,
			},
		)

	if err != nil {
		return Dashboard{}, fmt.Errorf(
			"get dashboard priorities: %w",
			err,
		)
	}

	assigneeRows, err :=
		r.queries.GetProjectDashboardByAssignee(
			ctx,
			db.GetProjectDashboardByAssigneeParams{
				TenantID: tenantID,

				ProjectID: pgProjectID,
			},
		)

	if err != nil {
		return Dashboard{}, fmt.Errorf(
			"get dashboard assignees: %w",
			err,
		)
	}
	result := Dashboard{
		Summary: Summary{
			TotalTasks: summaryRow.TotalTasks,

			CompletedTasks: summaryRow.CompletedTasks,

			OverdueTasks: summaryRow.OverdueTasks,

			UnassignedTasks: summaryRow.UnassignedTasks,

			TrackedSeconds: trackedSeconds,
		},

		ByStatus: make(
			[]StatusStat,
			0,
			len(statusRows),
		),

		ByPriority: make(
			[]PriorityStat,
			0,
			len(priorityRows),
		),

		ByAssignee: make(
			[]AssigneeStat,
			0,
			len(assigneeRows),
		),
	}

	for _, row := range statusRows {
		statusID := ""

		if row.StatusID.Valid {
			statusID = uuid.UUID(
				row.StatusID.Bytes,
			).String()
		}

		result.ByStatus = append(
			result.ByStatus,
			StatusStat{
				StatusID: statusID,

				Name: row.StatusName,

				Category: row.StatusCategory,

				TaskCount: row.TaskCount,
			},
		)
	}

	for _, row := range priorityRows {
		result.ByPriority = append(
			result.ByPriority,
			PriorityStat{
				Priority: row.Priority,

				TaskCount: row.TaskCount,
			},
		)
	}

	for _, row := range assigneeRows {
		var userID *string

		if row.AssigneeUserID.Valid {
			value :=
				row.AssigneeUserID.String

			userID = &value
		}

		result.ByAssignee = append(
			result.ByAssignee,
			AssigneeStat{
				UserID: userID,

				TaskCount: row.TaskCount,
			},
		)
	}

	return result, nil
}
