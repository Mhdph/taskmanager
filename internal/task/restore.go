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

func (r *Repository) Restore(ctx context.Context, tenantID, projectID, taskID string) (Task, error) {
	p, err := uuid.Parse(projectID)
	if err != nil {
		return Task{}, ErrInvalidID
	}
	id, err := uuid.Parse(taskID)
	if err != nil {
		return Task{}, ErrInvalidID
	}
	row, err := r.queries.RestoreTask(ctx, db.RestoreTaskParams{TenantID: tenantID, ProjectID: pgtype.UUID{Bytes: p, Valid: true}, ID: pgtype.UUID{Bytes: id, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("restore task: %w", err)
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
	item, err := s.repository.Restore(ctx, identity.TenantID, projectID, taskID)
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
