package task

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/project"
)

type labelResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

func toLabelResponse(label Label) labelResponse {
	return labelResponse{
		ID:    label.ID,
		Name:  label.Name,
		Color: label.Color,

		CreatedAt: label.CreatedAt.Unix(),
		UpdatedAt: label.UpdatedAt.Unix(),
	}
}

// -------------------------------------------------------------------------
// Label directory endpoints: /labels
// -------------------------------------------------------------------------

func (h *Handler) ListLabels(
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

	labels, err := h.service.ListLabels(
		r.Context(),
		identity,
	)
	if err != nil {
		log.Printf(
			"list labels: %v",
			err,
		)

		http.Error(
			w,
			"failed to list labels",
			http.StatusInternalServerError,
		)

		return
	}

	items := make(
		[]labelResponse,
		0,
		len(labels),
	)

	for _, label := range labels {
		items = append(
			items,
			toLabelResponse(label),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		map[string]any{"items": items},
	); err != nil {
		log.Printf(
			"encode labels response: %v",
			err,
		)
	}
}

type createLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (h *Handler) CreateLabel(
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

	var req createLabelRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	label, err := h.service.CreateLabel(
		r.Context(),
		CreateLabelInput{
			Identity: identity,

			Name:  req.Name,
			Color: req.Color,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		case errors.Is(err, ErrLabelNameRequired):
			http.Error(
				w,
				"label name is required",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrLabelInvalidColor):
			http.Error(
				w,
				"invalid label color",
				http.StatusBadRequest,
			)

		case isLabelConflict(err):
			http.Error(
				w,
				"label name already exists",
				http.StatusConflict,
			)

		default:
			log.Printf(
				"create label: %v",
				err,
			)

			http.Error(
				w,
				"failed to create label",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(
		toLabelResponse(label),
	); err != nil {
		log.Printf(
			"encode label response: %v",
			err,
		)
	}
}

type updateLabelRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

func (h *Handler) UpdateLabel(
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

	labelID := chi.URLParam(
		r,
		"labelID",
	)

	var req updateLabelRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	label, err := h.service.UpdateLabel(
		r.Context(),
		UpdateLabelInput{
			Identity: identity,
			LabelID:  labelID,

			Name:  req.Name,
			Color: req.Color,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrLabelNotFound):
			http.Error(
				w,
				"label not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		case errors.Is(err, ErrInvalidID),
			errors.Is(err, ErrLabelNameRequired),
			errors.Is(err, ErrLabelInvalidColor),
			errors.Is(err, ErrNoChanges):

			http.Error(
				w,
				"invalid request",
				http.StatusBadRequest,
			)

		case isLabelConflict(err):
			http.Error(
				w,
				"label name already exists",
				http.StatusConflict,
			)

		default:
			log.Printf(
				"update label: %v",
				err,
			)

			http.Error(
				w,
				"failed to update label",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		toLabelResponse(label),
	); err != nil {
		log.Printf(
			"encode label response: %v",
			err,
		)
	}
}

func (h *Handler) DeleteLabel(
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

	labelID := chi.URLParam(
		r,
		"labelID",
	)

	err := h.service.DeleteLabel(
		r.Context(),
		identity,
		labelID,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrLabelNotFound):
			http.Error(
				w,
				"label not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		case errors.Is(err, ErrInvalidID):
			http.Error(
				w,
				"invalid id",
				http.StatusBadRequest,
			)

		default:
			log.Printf(
				"delete label: %v",
				err,
			)

			http.Error(
				w,
				"failed to delete label",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// -------------------------------------------------------------------------
// Task label endpoints: /projects/{projectID}/tasks/{taskID}/labels
// -------------------------------------------------------------------------

type setTaskLabelsRequest struct {
	LabelIDs []string `json:"labelIds"`
}

func (h *Handler) GetTaskLabels(
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

	taskID := chi.URLParam(
		r,
		"taskID",
	)

	labels, err := h.service.GetTaskLabels(
		r.Context(),
		identity,
		projectID,
		taskID,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrTaskNotFound),
			errors.Is(err, project.ErrNotFound):

			http.Error(
				w,
				"task not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrInvalidID):
			http.Error(
				w,
				"invalid id",
				http.StatusBadRequest,
			)

		case errors.Is(err, project.ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"get task labels: %v",
				err,
			)

			http.Error(
				w,
				"failed to get task labels",
				http.StatusInternalServerError,
			)
		}

		return
	}

	items := make(
		[]labelResponse,
		0,
		len(labels),
	)

	for _, label := range labels {
		items = append(
			items,
			toLabelResponse(label),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		map[string]any{"items": items},
	); err != nil {
		log.Printf(
			"encode task labels response: %v",
			err,
		)
	}
}

func (h *Handler) SetTaskLabels(
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

	taskID := chi.URLParam(
		r,
		"taskID",
	)

	var req setTaskLabelsRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	labels, err := h.service.SetTaskLabels(
		r.Context(),
		SetTaskLabelsInput{
			Identity: identity,

			ProjectID: projectID,
			TaskID:    taskID,

			LabelIDs: req.LabelIDs,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrTaskNotFound),
			errors.Is(err, project.ErrNotFound):

			http.Error(
				w,
				"task not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrLabelNotFound):
			http.Error(
				w,
				"label not found",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrInvalidID):
			http.Error(
				w,
				"invalid id",
				http.StatusBadRequest,
			)

		case errors.Is(err, project.ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"set task labels: %v",
				err,
			)

			http.Error(
				w,
				"failed to set task labels",
				http.StatusInternalServerError,
			)
		}

		return
	}

	items := make(
		[]labelResponse,
		0,
		len(labels),
	)

	for _, label := range labels {
		items = append(
			items,
			toLabelResponse(label),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		map[string]any{"items": items},
	); err != nil {
		log.Printf(
			"encode task labels response: %v",
			err,
		)
	}
}

// -------------------------------------------------------------------------
// Reorder endpoint: /projects/{projectID}/tasks/reorder
// -------------------------------------------------------------------------

type reorderTaskRequest struct {
	TaskID   string  `json:"taskId"`
	Position float64 `json:"position"`
}

type reorderTasksRequest struct {
	Tasks []reorderTaskRequest `json:"tasks"`
}

func (h *Handler) Reorder(
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

	var req reorderTasksRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	tasks := make(
		[]ReorderTask,
		0,
		len(req.Tasks),
	)

	for _, item := range req.Tasks {
		if item.TaskID == "" {
			http.Error(
				w,
				"taskId is required",
				http.StatusBadRequest,
			)
			return
		}

		if math.IsNaN(item.Position) ||
			math.IsInf(item.Position, 0) {

			http.Error(
				w,
				"invalid position",
				http.StatusBadRequest,
			)
			return
		}

		tasks = append(
			tasks,
			ReorderTask{
				TaskID:   item.TaskID,
				Position: item.Position,
			},
		)
	}

	err := h.service.Reorder(
		r.Context(),
		ReorderInput{
			Identity: identity,

			ProjectID: projectID,

			Tasks: tasks,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrTaskNotFound):
			http.Error(
				w,
				"task not found",
				http.StatusNotFound,
			)

		case errors.Is(err, project.ErrNotFound):
			http.Error(
				w,
				"project not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrInvalidID),
			errors.Is(err, ErrNoChanges):

			http.Error(
				w,
				"invalid request",
				http.StatusBadRequest,
			)

		case errors.Is(err, project.ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"reorder tasks: %v",
				err,
			)

			http.Error(
				w,
				"failed to reorder tasks",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// isLabelConflict reports postgreSQL unique-violation errors surfaced by
// label creation and renames (duplicate name within the tenant).
func isLabelConflict(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "labels_tenant_id_name_key"
}
