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
	"github.com/mhdph/taskmanager/internal/project"
)

type createTaskRequest struct {
	StatusID string `json:"statusId"`

	ParentTaskID *string `json:"parentTaskId"`

	Title       string  `json:"title"`
	Description *string `json:"description"`

	Priority string `json:"priority"`

	AssigneeUserID *string `json:"assigneeUserId"`

	DueAt *time.Time `json:"dueAt"`
}

type taskResponse struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	StatusID  string `json:"statusId"`

	ParentTaskID *string `json:"parentTaskId"`

	Title       string  `json:"title"`
	Description *string `json:"description"`

	Priority string `json:"priority"`

	AssigneeUserID *string `json:"assigneeUserId"`

	DueAt       *time.Time `json:"dueAt"`
	CompletedAt *time.Time `json:"completedAt"`

	CreatedByUserID string `json:"createdByUserId"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toTaskResponse(
	task Task,
) taskResponse {
	return taskResponse{
		ID:        task.ID,
		ProjectID: task.ProjectID,
		StatusID:  task.StatusID,

		ParentTaskID: task.ParentTaskID,

		Title:       task.Title,
		Description: task.Description,

		Priority: string(task.Priority),

		AssigneeUserID: task.AssigneeUserID,

		DueAt:       task.DueAt,
		CompletedAt: task.CompletedAt,

		CreatedByUserID: task.CreatedByUserID,

		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}
}

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

func (h *Handler) Create(
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

	var req createTaskRequest

	if err := json.NewDecoder(r.Body).Decode(
		&req,
	); err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	created, err := h.service.Create(
		r.Context(),
		CreateInput{
			Identity: identity,

			ProjectID: projectID,
			StatusID:  req.StatusID,

			ParentTaskID: req.ParentTaskID,

			Title:       req.Title,
			Description: req.Description,

			Priority: Priority(req.Priority),

			AssigneeUserID: req.AssigneeUserID,

			DueAt: req.DueAt,
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrInvalidTitle,
		):
			http.Error(
				w,
				"invalid task title",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrInvalidPriority,
		):
			http.Error(
				w,
				"invalid task priority",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrStatusNotFound,
		):
			http.Error(
				w,
				"status not found",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrAssigneeNotMember,
		):
			http.Error(
				w,
				"assignee is not a project member",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrParentTaskNotFound,
		):
			http.Error(
				w,
				"parent task not found",
				http.StatusBadRequest,
			)

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
			project.ErrNotFound,
		):
			http.Error(
				w,
				"project not found",
				http.StatusNotFound,
			)

		default:
			log.Printf(
				"create task: %v",
				err,
			)

			http.Error(
				w,
				"failed to create task",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(
		http.StatusCreated,
	)

	if err := json.NewEncoder(w).Encode(
		toTaskResponse(created),
	); err != nil {
		log.Printf(
			"encode task response: %v",
			err,
		)
	}
}

type listTasksResponse struct {
	Items []taskResponse `json:"items"`

	NextCursor *string `json:"nextCursor"`
}

func (h *Handler) List(
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

	query := r.URL.Query()

	limit := 20

	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(
				w,
				"invalid limit",
				http.StatusBadRequest,
			)
			return
		}

		limit = value
	}

	page, err := h.service.List(
		r.Context(),
		ListInput{
			Identity:  identity,
			ProjectID: projectID,

			Limit:  limit,
			Cursor: query.Get("cursor"),

			StatusID: query.Get(
				"statusId",
			),

			AssigneeUserID: query.Get(
				"assigneeUserId",
			),

			Priority: query.Get(
				"priority",
			),

			ParentTaskID: query.Get(
				"parentTaskId",
			),

			Search: query.Get(
				"search",
			),
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrInvalidPriority,
		):
			http.Error(
				w,
				"invalid priority",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrInvalidID,
		):
			http.Error(
				w,
				"invalid id",
				http.StatusBadRequest,
			)

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
			project.ErrNotFound,
		):
			http.Error(
				w,
				"project not found",
				http.StatusNotFound,
			)

		default:
			log.Printf(
				"list tasks: %v",
				err,
			)

			http.Error(
				w,
				"failed to list tasks",
				http.StatusInternalServerError,
			)
		}

		return
	}

	items := make(
		[]taskResponse,
		0,
		len(page.Items),
	)

	for _, item := range page.Items {
		items = append(
			items,
			toTaskResponse(item),
		)
	}

	response := listTasksResponse{
		Items:      items,
		NextCursor: page.NextCursor,
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).
		Encode(response); err != nil {

		log.Printf(
			"encode task list: %v",
			err,
		)
	}
}
