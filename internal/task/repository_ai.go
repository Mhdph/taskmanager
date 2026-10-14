package task

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/mhdph/taskmanager/internal/database/generated"
)

type ProjectTaskForAI struct {
	ID             string
	Title          string
	Priority       Priority
	DueAt          *time.Time
	CompletedAt    *time.Time
	StatusName     string
	StatusCategory string
}

func (r *Repository) ListProjectTasksForAI(
	ctx context.Context,
	tenantID string,
	projectID string,
) ([]ProjectTaskForAI, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidID, err)
	}

	rows, err := r.queries.ListProjectTasksForAI(
		ctx,
		db.ListProjectTasksForAIParams{
			TenantID: tenantID,
			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list project tasks for AI: %w", err)
	}

	result := make([]ProjectTaskForAI, 0, len(rows))
	for _, row := range rows {
		item := ProjectTaskForAI{
			Title:          row.Title,
			Priority:       Priority(row.Priority),
			StatusName:     row.StatusName,
			StatusCategory: row.StatusCategory,
		}
		if row.ID.Valid {
			item.ID = uuid.UUID(row.ID.Bytes).String()
		}
		if row.DueAt.Valid {
			value := row.DueAt.Time
			item.DueAt = &value
		}
		if row.CompletedAt.Valid {
			value := row.CompletedAt.Time
			item.CompletedAt = &value
		}
		result = append(result, item)
	}

	return result, nil
}
