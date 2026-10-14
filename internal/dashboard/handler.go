package dashboard

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

type summaryResponse struct {
	TotalTasks int64 `json:"totalTasks"`

	CompletedTasks int64 `json:"completedTasks"`

	OverdueTasks int64 `json:"overdueTasks"`

	UnassignedTasks int64 `json:"unassignedTasks"`

	TrackedSeconds int64 `json:"trackedSeconds"`
}

type statusStatResponse struct {
	StatusID string `json:"statusId"`

	Name string `json:"name"`

	Category string `json:"category"`

	TaskCount int64 `json:"taskCount"`
}

type priorityStatResponse struct {
	Priority string `json:"priority"`

	TaskCount int64 `json:"taskCount"`
}

type assigneeStatResponse struct {
	UserID *string `json:"userId"`

	TaskCount int64 `json:"taskCount"`
}

type dashboardResponse struct {
	Summary summaryResponse `json:"summary"`

	ByStatus []statusStatResponse `json:"byStatus"`

	ByPriority []priorityStatResponse `json:"byPriority"`

	ByAssignee []assigneeStatResponse `json:"byAssignee"`
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

func (h *Handler) Get(
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

	projectID := chi.URLParam(
		r,
		"projectID",
	)

	result, err := h.service.Get(
		r.Context(),
		GetInput{
			Identity: identity,

			ProjectID: projectID,
		},
	)

	if err != nil {
		switch {
		case errors.Is(
			err,
			project.ErrInvalidID,
		):
			http.Error(
				w,
				"invalid project id",
				http.StatusBadRequest,
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

		case errors.Is(
			err,
			project.ErrForbidden,
		):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"dashboard: %v",
				err,
			)

			http.Error(
				w,
				"failed to get dashboard",
				http.StatusInternalServerError,
			)
		}

		return
	}
	statuses := make(
		[]statusStatResponse,
		0,
		len(result.ByStatus),
	)

	for _, item := range result.ByStatus {

		statuses = append(
			statuses,
			statusStatResponse{
				StatusID: item.StatusID,

				Name: item.Name,

				Category: item.Category,

				TaskCount: item.TaskCount,
			},
		)
	}

	priorities := make(
		[]priorityStatResponse,
		0,
		len(result.ByPriority),
	)

	for _, item := range result.ByPriority {

		priorities = append(
			priorities,
			priorityStatResponse{
				Priority: item.Priority,

				TaskCount: item.TaskCount,
			},
		)
	}

	assignees := make(
		[]assigneeStatResponse,
		0,
		len(result.ByAssignee),
	)

	for _, item := range result.ByAssignee {

		assignees = append(
			assignees,
			assigneeStatResponse{
				UserID: item.UserID,

				TaskCount: item.TaskCount,
			},
		)
	}

	response := dashboardResponse{
		Summary: summaryResponse{
			TotalTasks: result.Summary.TotalTasks,

			CompletedTasks: result.Summary.CompletedTasks,

			OverdueTasks: result.Summary.OverdueTasks,

			UnassignedTasks: result.Summary.UnassignedTasks,

			TrackedSeconds: result.Summary.TrackedSeconds,
		},

		ByStatus: statuses,

		ByPriority: priorities,

		ByAssignee: assignees,
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).
		Encode(response); err != nil {

		log.Printf(
			"encode dashboard: %v",
			err,
		)
	}
}
