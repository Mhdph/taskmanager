package member

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
)

func capacityIdentity(w http.ResponseWriter, r *http.Request) (auth.Identity, bool) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
	return identity, ok
}

func writeCapacityResult(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCapacity), errors.Is(err, ErrInvalidWorkload):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, ErrNotFound):
			http.Error(w, "member not found", http.StatusNotFound)
		default:
			log.Printf("member capacity: %v", err)
			http.Error(w, "capacity operation failed", http.StatusInternalServerError)
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
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode member capacity: %v", err)
	}
}

func (h *Handler) GetCapacity(w http.ResponseWriter, r *http.Request) {
	identity, ok := capacityIdentity(w, r)
	if !ok {
		return
	}
	value, err := h.service.GetCapacity(r.Context(), identity, chi.URLParam(r, "userID"))
	writeCapacityResult(w, http.StatusOK, value, err)
}

func (h *Handler) SetCapacity(w http.ResponseWriter, r *http.Request) {
	identity, ok := capacityIdentity(w, r)
	if !ok {
		return
	}
	var req struct {
		DailyCapacitySeconds *int32  `json:"dailyCapacitySeconds"`
		WorkingDays          []int16 `json:"workingDays"`
		Timezone             string  `json:"timezone"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
		return
	}
	if req.DailyCapacitySeconds == nil {
		writeCapacityResult(w, 0, nil, ErrInvalidCapacity)
		return
	}
	value, err := h.service.SetCapacity(r.Context(), CapacityInput{Identity: identity, UserID: chi.URLParam(r, "userID"), DailyCapacitySeconds: *req.DailyCapacitySeconds, WorkingDays: req.WorkingDays, Timezone: req.Timezone})
	writeCapacityResult(w, http.StatusOK, value, err)
}

func (h *Handler) ClearCapacity(w http.ResponseWriter, r *http.Request) {
	identity, ok := capacityIdentity(w, r)
	if !ok {
		return
	}
	err := h.service.ClearCapacity(r.Context(), identity, chi.URLParam(r, "userID"))
	writeCapacityResult(w, http.StatusNoContent, nil, err)
}

func (h *Handler) Workload(w http.ResponseWriter, r *http.Request) {
	identity, ok := capacityIdentity(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit := 50
	if value := q.Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeCapacityResult(w, 0, nil, ErrInvalidWorkload)
			return
		}
		limit = parsed
	}
	report, err := h.service.Workload(r.Context(), WorkloadInput{Identity: identity, UserID: chi.URLParam(r, "userID"), From: q.Get("from"), To: q.Get("to"), Timezone: q.Get("timezone"), Limit: limit, Cursor: q.Get("cursor")})
	writeCapacityResult(w, http.StatusOK, report, err)
}
