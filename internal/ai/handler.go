package ai

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

type Handler struct {
	service *Service
}

func NewHandler(
	service *Service,
) *Handler {
	return &Handler{
		service: service,
	}
}

type taskFromTextRequest struct {
	Text string `json:"text"`
}

func (h *Handler) TaskFromText(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok :=
		auth.IdentityFromContext(
			r.Context(),
		)

	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)
		return
	}

	var req taskFromTextRequest

	if err := json.NewDecoder(
		r.Body,
	).Decode(&req); err != nil {

		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	result, err :=
		h.service.TaskFromText(
			r.Context(),
			TaskFromTextInput{
				Identity: identity,

				Text: req.Text,
			},
		)

	if err != nil {
		if errors.Is(err, ErrProviderUnavailable) {
			http.Error(w, "ai provider unavailable", http.StatusServiceUnavailable)
			return
		}

		if errors.Is(
			err,
			ErrInvalidInput,
		) {
			http.Error(
				w,
				"invalid input",
				http.StatusBadRequest,
			)
			return
		}

		log.Printf(
			"ai task from text: %v",
			err,
		)

		http.Error(
			w,
			"ai request failed",
			http.StatusBadGateway,
		)
		return
	}

	writeJSON(w, result)
}

func writeJSON(
	w http.ResponseWriter,
	value any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).
		Encode(value); err != nil {

		log.Printf(
			"encode ai response: %v",
			err,
		)
	}
}

func (h *Handler) SummarizeTask(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok :=
		auth.IdentityFromContext(
			r.Context(),
		)

	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)
		return
	}

	projectID :=
		chi.URLParam(
			r,
			"projectID",
		)

	taskID :=
		chi.URLParam(
			r,
			"taskID",
		)

	result, err :=
		h.service.SummarizeTask(
			r.Context(),
			TaskActionInput{
				Identity: identity,

				ProjectID: projectID,

				TaskID: taskID,
			},
		)

	if err != nil {
		switch {
		case errors.Is(err, ErrProviderUnavailable):
			http.Error(w, "ai provider unavailable", http.StatusServiceUnavailable)

		case errors.Is(
			err,
			project.ErrForbidden,
		):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		case errors.Is(
			err,
			task.ErrTaskNotFound,
		):
			http.Error(
				w,
				"task not found",
				http.StatusNotFound,
			)

		default:
			log.Printf(
				"ai summarize task: %v",
				err,
			)

			http.Error(
				w,
				"ai request failed",
				http.StatusBadGateway,
			)
		}

		return
	}

	writeJSON(
		w,
		result,
	)
}
