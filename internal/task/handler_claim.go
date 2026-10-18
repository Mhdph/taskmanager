package task

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/project"
)

// Claim assigns only to the authenticated user; identity is never accepted in the body.
func (h *Handler) Claim(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		StatusID *string `json:"statusId"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
		return
	}
	item, err := h.service.Update(r.Context(), UpdateInput{
		Identity: identity, ProjectID: chi.URLParam(r, "projectID"),
		TaskID: chi.URLParam(r, "taskID"), ClaimOnly: true, StatusID: req.StatusID,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrTaskAlreadyAssigned):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, ErrInvalidID), errors.Is(err, project.ErrInvalidID):
			http.Error(w, "invalid id", http.StatusBadRequest)
		case errors.Is(err, ErrTaskNotFound), errors.Is(err, ErrStatusNotFound), errors.Is(err, project.ErrNotFound):
			http.Error(w, "task, project, or status not found", http.StatusNotFound)
		case errors.Is(err, project.ErrForbidden), errors.Is(err, ErrAssigneeNotMember):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			log.Printf("claim task: %v", err)
			http.Error(w, "failed to claim task", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(toTaskResponse(item))
}
