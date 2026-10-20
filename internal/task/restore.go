package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mhdph/taskmanager/internal/audit"
	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/project"
)

// Restore unarchives a task. A task that belonged to a completed sprint
// returns to the backlog instead, and the membership history records the
// release.
func (r *Repository) Restore(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	actorUserID string,
) (Task, error) {
	p, err := uuid.Parse(projectID)
	if err != nil {
		return Task{}, ErrInvalidID
	}
	id, err := uuid.Parse(taskID)
	if err != nil {
		return Task{}, ErrInvalidID
	}

	projectUUID := pgtype.UUID{Bytes: p, Valid: true}
	taskUUID := pgtype.UUID{Bytes: id, Valid: true}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Task{}, fmt.Errorf("begin task restore: %w", err)
	}

	defer tx.Rollback(ctx)

	queries := r.queries.WithTx(tx)

	if err := queries.RecordSprintReleaseOnRestore(ctx, db.RecordSprintReleaseOnRestoreParams{
		ID:          pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ActorUserID: actorUserID,
		TenantID:    tenantID,
		ProjectID:   projectUUID,
		TaskID:      taskUUID,
	}); err != nil {
		return Task{}, fmt.Errorf("record sprint release: %w", err)
	}

	row, err := queries.RestoreTask(ctx, db.RestoreTaskParams{
		TenantID:  tenantID,
		ProjectID: projectUUID,
		ID:        taskUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("restore task: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Task{}, fmt.Errorf("commit task restore: %w", err)
	}

	return taskFromDB(row), nil
}

func (s *Service) Restore(ctx context.Context, identity auth.Identity, projectID, taskID string) (Task, error) {
	if err := s.projectAuthorizer.Require(ctx, identity, projectID, project.PermissionArchiveTask); err != nil {
		return Task{}, err
	}
	if _, err := s.projectRepository.GetForTenant(ctx, identity.TenantID, projectID); err != nil {
		return Task{}, err
	}
	item, err := s.repository.Restore(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
		identity.UserID,
	)
	if err != nil {
		return Task{}, err
	}
	s.recordAudit(ctx, audit.RecordInput{TenantID: identity.TenantID, ActorUserID: identity.UserID, Action: audit.ActionTaskRestored, EntityType: audit.EntityTask, EntityID: item.ID, ProjectID: auditID(projectID), TaskID: auditID(taskID)})
	return item, nil
}

func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	item, err := h.service.Restore(r.Context(), identity, chi.URLParam(r, "projectID"), chi.URLParam(r, "taskID"))
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID), errors.Is(err, project.ErrInvalidID):
			http.Error(w, "invalid id", http.StatusBadRequest)
		case errors.Is(err, project.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, ErrTaskNotFound), errors.Is(err, project.ErrNotFound):
			http.Error(w, "archived task or active project not found", http.StatusNotFound)
		default:
			log.Printf("restore task: %v", err)
			http.Error(w, "failed to restore task", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toTaskResponse(item)); err != nil {
		log.Printf("encode restored task: %v", err)
	}
}
