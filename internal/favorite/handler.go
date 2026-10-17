package favorite

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

type listResponse struct {
	TaskIDs    []string `json:"taskIds"`
	ProjectIDs []string `json:"projectIds"`
}

func (h *Handler) Get(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(
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

	output, err := h.service.List(
		r.Context(),
		identity,
	)
	if err != nil {
		log.Printf(
			"list favorites: %v",
			err,
		)

		http.Error(
			w,
			"failed to list favorites",
			http.StatusInternalServerError,
		)

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		listResponse{
			TaskIDs:    output.TaskIDs,
			ProjectIDs: output.ProjectIDs,
		},
	); err != nil {
		log.Printf(
			"encode favorites response: %v",
			err,
		)
	}
}

func (h *Handler) AddTask(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(
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

	taskID := chi.URLParam(
		r,
		"taskID",
	)

	err := h.service.AddTask(
		r.Context(),
		identity,
		taskID,
	)
	if err != nil {
		h.writeTaskError(
			w,
			"add favorite task",
			err,
		)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveTask(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(
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

	taskID := chi.URLParam(
		r,
		"taskID",
	)

	err := h.service.RemoveTask(
		r.Context(),
		identity,
		taskID,
	)
	if err != nil {
		h.writeTaskError(
			w,
			"remove favorite task",
			err,
		)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddProject(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(
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

	projectID := chi.URLParam(
		r,
		"projectID",
	)

	err := h.service.AddProject(
		r.Context(),
		identity,
		projectID,
	)
	if err != nil {
		h.writeProjectError(
			w,
			"add favorite project",
			err,
		)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveProject(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(
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

	projectID := chi.URLParam(
		r,
		"projectID",
	)

	err := h.service.RemoveProject(
		r.Context(),
		identity,
		projectID,
	)
	if err != nil {
		h.writeProjectError(
			w,
			"remove favorite project",
			err,
		)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeTaskError maps errors for the task favorite endpoints.
func (h *Handler) writeTaskError(
	w http.ResponseWriter,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, task.ErrTaskNotFound):
		http.Error(
			w,
			"task not found",
			http.StatusNotFound,
		)

	case errors.Is(err, project.ErrNotFound):
		http.Error(
			w,
			"task not found",
			http.StatusNotFound,
		)

	case errors.Is(err, project.ErrForbidden):
		http.Error(
			w,
			"forbidden",
			http.StatusForbidden,
		)

	case errors.Is(err, ErrInvalidID),
		errors.Is(err, task.ErrInvalidID):

		http.Error(
			w,
			"invalid id",
			http.StatusBadRequest,
		)

	default:
		log.Printf(
			"%s: %v",
			action,
			err,
		)

		http.Error(
			w,
			"failed to update favorites",
			http.StatusInternalServerError,
		)
	}
}

// writeProjectError maps errors for the project favorite endpoints.
func (h *Handler) writeProjectError(
	w http.ResponseWriter,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, ErrNotFound),
		errors.Is(err, project.ErrNotFound):

		http.Error(
			w,
			"project not found",
			http.StatusNotFound,
		)

	case errors.Is(err, project.ErrForbidden):
		http.Error(
			w,
			"forbidden",
			http.StatusForbidden,
		)

	case errors.Is(err, ErrInvalidID),
		errors.Is(err, project.ErrInvalidID):

		http.Error(
			w,
			"invalid id",
			http.StatusBadRequest,
		)

	default:
		log.Printf(
			"%s: %v",
			action,
			err,
		)

		http.Error(
			w,
			"failed to update favorites",
			http.StatusInternalServerError,
		)
	}
}
