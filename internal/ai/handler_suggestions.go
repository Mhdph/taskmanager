package ai

import (
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

func (h *Handler) SuggestTask(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	result, err := h.service.SuggestTask(r.Context(), TaskActionInput{
		Identity:  identity,
		ProjectID: chi.URLParam(r, "projectID"),
		TaskID:    chi.URLParam(r, "taskID"),
	})
	if err != nil {
		switch {
		case errors.Is(err, project.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, task.ErrTaskNotFound):
			http.Error(w, "task not found", http.StatusNotFound)
		case errors.Is(err, task.ErrInvalidID), errors.Is(err, project.ErrInvalidID):
			http.Error(w, "invalid id", http.StatusBadRequest)
		case errors.Is(err, ErrProviderUnavailable):
			http.Error(w, "ai provider unavailable", http.StatusServiceUnavailable)
		default:
			log.Printf("ai suggest task: %v", err)
			http.Error(w, "ai request failed", http.StatusBadGateway)
		}
		return
	}

	writeJSON(w, result)
}
