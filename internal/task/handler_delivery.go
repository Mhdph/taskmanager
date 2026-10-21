package task

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/project"
)

func deliveryInput(w http.ResponseWriter, r *http.Request) (DeliveryInput, bool) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return DeliveryInput{}, false
	}
	return DeliveryInput{Identity: identity, ProjectID: chi.URLParam(r, "projectID"), TaskID: chi.URLParam(r, "taskID")}, true
}

func writeDeliveryResult(w http.ResponseWriter, status int, result any, err error) {
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID), errors.Is(err, project.ErrInvalidID), errors.Is(err, ErrInvalidDelivery), errors.Is(err, ErrInvalidDeliveryReview), errors.Is(err, ErrInvalidDeliveryReviewer):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, project.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, ErrTaskNotFound), errors.Is(err, ErrDeliveryNotFound), errors.Is(err, project.ErrNotFound):
			http.Error(w, "task, project or delivery not found", http.StatusNotFound)
		case errors.Is(err, ErrDeliveryPending), errors.Is(err, ErrDeliveryResolved):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			log.Printf("task delivery: %v", err)
			http.Error(w, "delivery operation failed", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("encode task delivery: %v", err)
	}
}

func (h *Handler) SubmitDelivery(w http.ResponseWriter, r *http.Request) {
	input, ok := deliveryInput(w, r)
	if !ok {
		return
	}
	var request struct {
		ReviewerUserID string  `json:"reviewerUserId"`
		Body           string  `json:"body"`
		OutputURL      *string `json:"outputUrl"`
	}
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
		return
	}
	delivery, err := h.service.SubmitDelivery(r.Context(), SubmitDeliveryInput{
		DeliveryInput: input, ReviewerUserID: request.ReviewerUserID, Body: request.Body, OutputURL: request.OutputURL,
	})
	writeDeliveryResult(w, http.StatusCreated, delivery, err)
}

func (h *Handler) ReviewDelivery(w http.ResponseWriter, r *http.Request) {
	input, ok := deliveryInput(w, r)
	if !ok {
		return
	}
	var request struct {
		Decision DeliveryStatus `json:"decision"`
		Note     string         `json:"note"`
	}
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
		return
	}
	delivery, err := h.service.ReviewDelivery(r.Context(), ReviewDeliveryInput{
		DeliveryInput: input, DeliveryID: chi.URLParam(r, "deliveryID"), Decision: request.Decision, Note: request.Note,
	})
	writeDeliveryResult(w, http.StatusOK, delivery, err)
}

func (h *Handler) WithdrawDelivery(w http.ResponseWriter, r *http.Request) {
	input, ok := deliveryInput(w, r)
	if !ok {
		return
	}
	var request struct {
		Note string `json:"note"`
	}
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
		return
	}
	delivery, err := h.service.WithdrawDelivery(r.Context(), ReviewDeliveryInput{
		DeliveryInput: input, DeliveryID: chi.URLParam(r, "deliveryID"), Note: request.Note,
	})
	writeDeliveryResult(w, http.StatusOK, delivery, err)
}

func (h *Handler) GetDelivery(w http.ResponseWriter, r *http.Request) {
	input, ok := deliveryInput(w, r)
	if !ok {
		return
	}
	delivery, err := h.service.GetDelivery(r.Context(), input, chi.URLParam(r, "deliveryID"))
	writeDeliveryResult(w, http.StatusOK, delivery, err)
}

func (h *Handler) ListDeliveries(w http.ResponseWriter, r *http.Request) {
	input, ok := deliveryInput(w, r)
	if !ok {
		return
	}
	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeDeliveryResult(w, 0, nil, ErrInvalidID)
			return
		}
		limit = parsed
	}
	page, err := h.service.ListDeliveries(r.Context(), input, limit, r.URL.Query().Get("cursor"))
	writeDeliveryResult(w, http.StatusOK, page, err)
}
