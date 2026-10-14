package ai

import (
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

func (h *Handler) SummarizeProject(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	result, err := h.service.SummarizeProject(r.Context(), ProjectSummaryInput{Identity: identity, ProjectID: chi.URLParam(r, "projectID")})
	if err != nil {
		switch {
		case errors.Is(err, project.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, project.ErrNotFound):
			http.Error(w, "project not found", http.StatusNotFound)
		case errors.Is(err, project.ErrInvalidID), errors.Is(err, ErrInvalidInput):
			http.Error(w, "invalid input", http.StatusBadRequest)
		case errors.Is(err, ErrProviderUnavailable):
			http.Error(w, "ai provider unavailable", http.StatusServiceUnavailable)
		default:
			log.Printf("ai summarize project: %v", err)
			http.Error(w, "ai request failed", http.StatusBadGateway)
		}
		return
	}
	writeJSON(w, result)
}
