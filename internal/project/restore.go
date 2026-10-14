package project

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
	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

func (r *Repository) Restore(ctx context.Context, tenantID, projectID string) (Project, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return Project{}, ErrInvalidID
	}
	row, err := r.queries.RestoreProject(ctx, db.RestoreProjectParams{TenantID: tenantID, ID: pgtype.UUID{Bytes: id, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("restore project: %w", err)
	}
	return projectFromDB(row), nil
}

func (s *Service) Restore(ctx context.Context, identity auth.Identity, projectID string) (Project, error) {
	if err := s.authorizer.Require(ctx, identity, projectID, PermissionArchiveProject); err != nil {
		return Project{}, err
	}
	return s.repository.Restore(ctx, identity.TenantID, projectID)
}

func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	item, err := h.service.Restore(r.Context(), identity, chi.URLParam(r, "projectID"))
	if err != nil {
		handleProjectError(w, "failed to restore project", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toProjectResponse(item)); err != nil {
		log.Printf("encode restored project: %v", err)
	}
}
