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

type createTaskRequest struct {
	StatusID string `json:"statusId"`

	ParentTaskID *string `json:"parentTaskId"`

	Title       string  `json:"title"`
	Description *string `json:"description"`

	Priority string `json:"priority"`

	AssigneeUserID *string `json:"assigneeUserId"`

	DueAt *time.Time `json:"dueAt"`
}

type updateTaskRequest struct {
	Title *string `json:"title"`

	Description json.RawMessage `json:"description"`

	StatusID *string `json:"statusId"`

	Priority *string `json:"priority"`

	AssigneeUserID json.RawMessage `json:"assigneeUserId"`

	ParentTaskID json.RawMessage `json:"parentTaskId"`

	DueAt json.RawMessage `json:"dueAt"`
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

type listTasksResponse struct {
	Items      []taskResponse `json:"items"`
	NextCursor *string        `json:"nextCursor"`
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
		case errors.Is(err, ErrInvalidTitle):
			http.Error(
				w,
				"invalid task title",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrInvalidPriority):
			http.Error(
				w,
				"invalid task priority",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrStatusNotFound):
			http.Error(
				w,
				"status not found",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrAssigneeNotMember):
			http.Error(
				w,
				"assignee is not a project member",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrParentTaskNotFound):
			http.Error(
				w,
				"parent task not found",
				http.StatusBadRequest,
			)

		case errors.Is(err, project.ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		case errors.Is(err, project.ErrNotFound):
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
		case errors.Is(err, ErrInvalidPriority):
			http.Error(
				w,
				"invalid priority",
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

		case errors.Is(err, project.ErrNotFound):
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

	if err := json.NewEncoder(w).Encode(
		response,
	); err != nil {
		log.Printf(
			"encode task list: %v",
			err,
		)
	}
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

	projectID := chi.URLParam(
		r,
		"projectID",
	)

	taskID := chi.URLParam(
		r,
		"taskID",
	)

	item, err := h.service.Get(
		r.Context(),
		identity,
		projectID,
		taskID,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID):
			http.Error(
				w,
				"invalid id",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrTaskNotFound):
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

		default:
			log.Printf(
				"get task: %v",
				err,
			)

			http.Error(
				w,
				"failed to get task",
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
		toTaskResponse(item),
	); err != nil {
		log.Printf(
			"encode task response: %v",
			err,
		)
	}
}

func (h *Handler) Update(
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

	var req updateTaskRequest

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

	updateDescription,
		description,
		err := parseNullableString(
		req.Description,
	)
	if err != nil {
		http.Error(
			w,
			"invalid description",
			http.StatusBadRequest,
		)
		return
	}

	updateAssignee,
		assigneeUserID,
		err := parseNullableString(
		req.AssigneeUserID,
	)
	if err != nil {
		http.Error(
			w,
			"invalid assigneeUserId",
			http.StatusBadRequest,
		)
		return
	}

	updateParentTask,
		parentTaskID,
		err := parseNullableString(
		req.ParentTaskID,
	)
	if err != nil {
		http.Error(
			w,
			"invalid parentTaskId",
			http.StatusBadRequest,
		)
		return
	}

	updateDueAt,
		dueAt,
		err := parseNullableTime(
		req.DueAt,
	)
	if err != nil {
		http.Error(
			w,
			"invalid dueAt",
			http.StatusBadRequest,
		)
		return
	}

	var priority *Priority

	if req.Priority != nil {
		value := Priority(
			*req.Priority,
		)

		priority = &value
	}

	updated, err := h.service.Update(
		r.Context(),
		UpdateInput{
			Identity: identity,

			ProjectID: projectID,
			TaskID:    taskID,

			Title: req.Title,

			UpdateDescription: updateDescription,
			Description:       description,

			StatusID: req.StatusID,

			Priority: priority,

			UpdateAssignee: updateAssignee,
			AssigneeUserID: assigneeUserID,

			UpdateParentTask: updateParentTask,
			ParentTaskID:     parentTaskID,

			UpdateDueAt: updateDueAt,
			DueAt:       dueAt,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID),
			errors.Is(err, ErrInvalidTitle),
			errors.Is(err, ErrInvalidPriority),
			errors.Is(err, ErrInvalidParentTask),
			errors.Is(err, ErrNoChanges):

			http.Error(
				w,
				"invalid request",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrStatusNotFound):
			http.Error(
				w,
				"status not found",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrAssigneeNotMember):
			http.Error(
				w,
				"assignee is not a project member",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrParentTaskNotFound):
			http.Error(
				w,
				"parent task not found",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrTaskNotFound):
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

		default:
			log.Printf(
				"update task: %v",
				err,
			)

			http.Error(
				w,
				"failed to update task",
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
		toTaskResponse(updated),
	); err != nil {
		log.Printf(
			"encode task response: %v",
			err,
		)
	}
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

func parseNullableString(
	raw json.RawMessage,
) (
	provided bool,
	value *string,
	err error,
) {
	if raw == nil {
		return false, nil, nil
	}

	if string(raw) == "null" {
		return true, nil, nil
	}

	var decoded string

	if err := json.Unmarshal(
		raw,
		&decoded,
	); err != nil {
		return false, nil, err
	}

	return true, &decoded, nil
}

func parseNullableTime(
	raw json.RawMessage,
) (
	provided bool,
	value *time.Time,
	err error,
) {
	if raw == nil {
		return false, nil, nil
	}

	if string(raw) == "null" {
		return true, nil, nil
	}

	var decoded time.Time

	if err := json.Unmarshal(
		raw,
		&decoded,
	); err != nil {
		return false, nil, err
	}

	return true, &decoded, nil
}

func (h *Handler) Archive(
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

	err := h.service.Archive(
		r.Context(),
		identity,
		projectID,
		taskID,
	)
	if err != nil {
		switch {
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
			ErrTaskNotFound,
		):
			http.Error(
				w,
				"task not found",
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
				"archive task: %v",
				err,
			)

			http.Error(
				w,
				"failed to archive task",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(
		http.StatusNoContent,
	)
}

type addTaskDependencyRequest struct {
	DependsOnTaskID string `json:"dependsOnTaskId"`
}

type taskDependencyResponse struct {
	TaskID          string    `json:"taskId"`
	DependsOnTaskID string    `json:"dependsOnTaskId"`
	CreatedAt       time.Time `json:"createdAt"`
}

func toTaskDependencyResponse(
	dependency Dependency,
) taskDependencyResponse {
	return taskDependencyResponse{
		TaskID: dependency.TaskID,

		DependsOnTaskID: dependency.DependsOnTaskID,

		CreatedAt: dependency.CreatedAt,
	}
}

func (h *Handler) AddDependency(
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

	var req addTaskDependencyRequest

	if err := json.NewDecoder(r.Body).
		Decode(&req); err != nil {

		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	dependency, err := h.service.AddDependency(
		r.Context(),
		AddDependencyInput{
			Identity: identity,

			ProjectID: projectID,
			TaskID:    taskID,

			DependsOnTaskID: req.DependsOnTaskID,
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrInvalidID,
		),
			errors.Is(
				err,
				ErrSelfDependency,
			):

			http.Error(
				w,
				"invalid dependency",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrTaskNotFound,
		):
			http.Error(
				w,
				"task not found",
				http.StatusNotFound,
			)

		case errors.Is(
			err,
			ErrDependencyExists,
		):
			http.Error(
				w,
				"dependency already exists",
				http.StatusConflict,
			)

		case errors.Is(
			err,
			ErrDependencyCycle,
		):
			http.Error(
				w,
				"dependency would create a cycle",
				http.StatusConflict,
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
				"add task dependency: %v",
				err,
			)

			http.Error(
				w,
				"failed to add dependency",
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
		toTaskDependencyResponse(
			dependency,
		),
	); err != nil {
		log.Printf(
			"encode dependency response: %v",
			err,
		)
	}
}

func (h *Handler) ListDependencies(
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

	dependencies, err :=
		h.service.ListDependencies(
			r.Context(),
			identity,
			projectID,
			taskID,
		)

	if err != nil {
		switch {
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
			ErrTaskNotFound,
		):
			http.Error(
				w,
				"task not found",
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
				"list task dependencies: %v",
				err,
			)

			http.Error(
				w,
				"failed to list dependencies",
				http.StatusInternalServerError,
			)
		}

		return
	}

	response := make(
		[]taskDependencyResponse,
		0,
		len(dependencies),
	)

	for _, dependency := range dependencies {

		response = append(
			response,
			toTaskDependencyResponse(
				dependency,
			),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).
		Encode(response); err != nil {

		log.Printf(
			"encode dependencies: %v",
			err,
		)
	}
}

func (h *Handler) RemoveDependency(
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

	dependsOnTaskID := chi.URLParam(
		r,
		"dependsOnTaskID",
	)

	err := h.service.RemoveDependency(
		r.Context(),
		RemoveDependencyInput{
			Identity: identity,

			ProjectID: projectID,
			TaskID:    taskID,

			DependsOnTaskID: dependsOnTaskID,
		},
	)
	if err != nil {
		switch {
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
			ErrDependencyNotFound,
		):
			http.Error(
				w,
				"dependency not found",
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
				"remove task dependency: %v",
				err,
			)

			http.Error(
				w,
				"failed to remove dependency",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(
		http.StatusNoContent,
	)
}

type addTaskWatcherRequest struct {
	UserID string `json:"userId"`
}

type taskWatcherResponse struct {
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
}

func toTaskWatcherResponse(
	watcher Watcher,
) taskWatcherResponse {
	return taskWatcherResponse{
		UserID:    watcher.UserID,
		CreatedAt: watcher.CreatedAt,
	}
}

func (h *Handler) AddWatcher(
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

	taskID := chi.URLParam(
		r,
		"taskID",
	)

	var req addTaskWatcherRequest

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

	watcher, err :=
		h.service.AddWatcher(
			r.Context(),
			AddWatcherInput{
				Identity: identity,

				ProjectID: projectID,

				TaskID: taskID,

				UserID: req.UserID,
			},
		)

	if err != nil {
		switch {
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
			ErrWatcherNotMember,
		):
			http.Error(
				w,
				"watcher is not a project member",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrWatcherExists,
		):
			http.Error(
				w,
				"watcher already exists",
				http.StatusConflict,
			)

		case errors.Is(
			err,
			ErrTaskNotFound,
		):
			http.Error(
				w,
				"task not found",
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
				"add task watcher: %v",
				err,
			)

			http.Error(
				w,
				"failed to add watcher",
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

	if err := json.NewEncoder(w).
		Encode(
			toTaskWatcherResponse(
				watcher,
			),
		); err != nil {

		log.Printf(
			"encode watcher: %v",
			err,
		)
	}
}

func (h *Handler) ListWatchers(
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

	taskID := chi.URLParam(
		r,
		"taskID",
	)

	items, err :=
		h.service.ListWatchers(
			r.Context(),
			identity,
			projectID,
			taskID,
		)

	if err != nil {
		switch {
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
			ErrTaskNotFound,
		):
			http.Error(
				w,
				"task not found",
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
				"list task watchers: %v",
				err,
			)

			http.Error(
				w,
				"failed to list watchers",
				http.StatusInternalServerError,
			)
		}

		return
	}

	response := make(
		[]taskWatcherResponse,
		0,
		len(items),
	)

	for _, item := range items {
		response = append(
			response,
			toTaskWatcherResponse(item),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).
		Encode(response); err != nil {

		log.Printf(
			"encode watchers: %v",
			err,
		)
	}
}

func (h *Handler) RemoveWatcher(
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

	taskID := chi.URLParam(
		r,
		"taskID",
	)

	userID := chi.URLParam(
		r,
		"userID",
	)

	err := h.service.RemoveWatcher(
		r.Context(),
		RemoveWatcherInput{
			Identity: identity,

			ProjectID: projectID,

			TaskID: taskID,

			UserID: userID,
		},
	)

	if err != nil {
		switch {
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
			ErrWatcherNotFound,
		):
			http.Error(
				w,
				"watcher not found",
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
				"remove task watcher: %v",
				err,
			)

			http.Error(
				w,
				"failed to remove watcher",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(
		http.StatusNoContent,
	)
}
