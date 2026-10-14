package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/project"
)

// Hold the status category stable until the task mutation commits. This shares
// the row lock used by category edits, including when a status has no tasks yet.
func lockTaskStatus(ctx context.Context, queries *db.Queries, tenantID string, projectID uuid.UUID, statusID pgtype.UUID) (project.StatusCategory, error) {
	status, err := queries.LockProjectStatusForTask(ctx, db.LockProjectStatusForTaskParams{
		TenantID:  tenantID,
		ProjectID: pgtype.UUID{Bytes: projectID, Valid: true},
		ID:        statusID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrStatusNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock task status: %w", err)
	}
	return project.StatusCategory(status.Category), nil
}
