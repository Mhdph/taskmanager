package task

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

func timeInput(w http.ResponseWriter, r *http.Request) (TimeInput, bool) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return TimeInput{}, false
	}
	return TimeInput{Identity: identity, ProjectID: chi.URLParam(r, "projectID"), TaskID: chi.URLParam(r, "taskID")}, true
}

func writeTimeResult(w http.ResponseWriter, status int, result any, err error) {
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID), errors.Is(err, project.ErrInvalidID), errors.Is(err, project.ErrInvalidCursor):
			http.Error(w, "invalid request", http.StatusBadRequest)
		case errors.Is(err, project.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, ErrTaskNotFound), errors.Is(err, project.ErrNotFound), errors.Is(err, ErrTimerNotFound), errors.Is(err, ErrTimeEntryNotFound):
			http.Error(w, "task or time entry not found", http.StatusNotFound)
		case errors.Is(err, ErrTimerActive):
			http.Error(w, ErrTimerActive.Error(), http.StatusConflict)
		default:
			log.Printf("task time entry: %v", err)
			http.Error(w, "time entry operation failed", http.StatusInternalServerError)
		}
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("encode time entry: %v", err)
	}
}

func (h *Handler) StartTimer(w http.ResponseWriter, r *http.Request) {
	input, ok := timeInput(w, r)
	if !ok {
		return
	}
	entry, err := h.service.StartTimer(r.Context(), input)
	writeTimeResult(w, http.StatusCreated, entry, err)
}

func (h *Handler) StopTimer(w http.ResponseWriter, r *http.Request) {
	input, ok := timeInput(w, r)
	if !ok {
		return
	}
	entry, err := h.service.StopTimer(r.Context(), input)
	writeTimeResult(w, http.StatusOK, entry, err)
}

func (h *Handler) DeleteTimeEntry(w http.ResponseWriter, r *http.Request) {
	input, ok := timeInput(w, r)
	if !ok {
		return
	}
	err := h.service.DeleteTimeEntry(r.Context(), input, chi.URLParam(r, "entryID"))
	writeTimeResult(w, http.StatusNoContent, nil, err)
}

func (h *Handler) ListTimeEntries(w http.ResponseWriter, r *http.Request) {
	input, ok := timeInput(w, r)
	if !ok {
		return
	}
	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			writeTimeResult(w, 0, nil, ErrInvalidID)
			return
		}
	}
	page, err := h.service.ListTimeEntries(r.Context(), input, limit, r.URL.Query().Get("cursor"))
	writeTimeResult(w, http.StatusOK, page, err)
}
