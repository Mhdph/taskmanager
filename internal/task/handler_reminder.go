package task

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/project"
)

func reminderInput(w http.ResponseWriter, r *http.Request) (ReminderInput, bool) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return ReminderInput{}, false
	}
	return ReminderInput{Identity: identity, ProjectID: chi.URLParam(r, "projectID"), TaskID: chi.URLParam(r, "taskID")}, true
}

func writeReminderResult(w http.ResponseWriter, status int, result any, err error) {
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID), errors.Is(err, ErrInvalidReminder):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, project.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, project.ErrNotFound), errors.Is(err, ErrTaskNotFound), errors.Is(err, ErrReminderNotFound):
			http.Error(w, "task or reminder not found", http.StatusNotFound)
		case errors.Is(err, ErrReminderLimit), errors.Is(err, ErrReminderCancelled), errors.Is(err, ErrReminderCompletedTask):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			log.Printf("task reminder: %v", err)
			http.Error(w, "reminder operation failed", http.StatusInternalServerError)
		}
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("encode reminder: %v", err)
	}
}

func (h *Handler) CreateReminder(w http.ResponseWriter, r *http.Request) {
	input, ok := reminderInput(w, r)
	if !ok {
		return
	}
	var request struct {
		RemindAt         *time.Time `json:"remindAt"`
		MinutesBeforeDue *int32     `json:"minutesBeforeDue"`
		Note             string     `json:"note"`
	}
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
		return
	}
	item, err := h.service.CreateReminder(r.Context(), CreateReminderInput{ReminderInput: input, RemindAt: request.RemindAt, MinutesBeforeDue: request.MinutesBeforeDue, Note: request.Note})
	writeReminderResult(w, http.StatusCreated, item, err)
}

func (h *Handler) SnoozeReminder(w http.ResponseWriter, r *http.Request) {
	input, ok := reminderInput(w, r)
	if !ok {
		return
	}
	var request struct {
		RemindAt time.Time `json:"remindAt"`
	}
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
		return
	}
	item, err := h.service.SnoozeReminder(r.Context(), input, chi.URLParam(r, "reminderID"), request.RemindAt)
	writeReminderResult(w, http.StatusOK, item, err)
}

func (h *Handler) CancelReminder(w http.ResponseWriter, r *http.Request) {
	input, ok := reminderInput(w, r)
	if !ok {
		return
	}
	err := h.service.CancelReminder(r.Context(), input, chi.URLParam(r, "reminderID"))
	writeReminderResult(w, http.StatusNoContent, nil, err)
}

func (h *Handler) ListReminders(w http.ResponseWriter, r *http.Request) {
	input, ok := reminderInput(w, r)
	if !ok {
		return
	}
	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeReminderResult(w, 0, nil, ErrInvalidID)
			return
		}
		limit = parsed
	}
	page, err := h.service.ListReminders(r.Context(), input, limit, r.URL.Query().Get("cursor"))
	writeReminderResult(w, http.StatusOK, page, err)
}
