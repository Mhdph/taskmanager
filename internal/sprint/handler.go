package sprint

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/project"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// -------------------------------------------------------------------------
// DTOs
// -------------------------------------------------------------------------

type createSprintRequest struct {
	Name string  `json:"name"`
	Goal *string `json:"goal"`

	PlannedStartAt *time.Time `json:"plannedStartAt"`
	PlannedEndAt   *time.Time `json:"plannedEndAt"`
}

type updateSprintRequest struct {
	Name *string `json:"name"`

	Goal      json.RawMessage `json:"goal"`
	GoalIsSet bool            `json:"-"`

	PlannedStartAt    json.RawMessage `json:"plannedStartAt"`
	PlannedStartIsSet bool            `json:"-"`

	PlannedEndAt *time.Time `json:"plannedEndAt"`
}

type completeSprintRequest struct {
	CarryoverSprintID *string `json:"carryoverSprintId"`
}

type setTaskSprintRequest struct {
	SprintID *string `json:"sprintId"`
}

type listSprintsResponse struct {
	Items      []sprintResponse `json:"items"`
	NextCursor *string          `json:"nextCursor"`
}

type sprintResponse struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`

	Name string  `json:"name"`
	Goal *string `json:"goal,omitempty"`

	PlannedStartAt *time.Time `json:"plannedStartAt,omitempty"`
	PlannedEndAt   time.Time  `json:"plannedEndAt"`

	State string `json:"state"`

	CreatedByUserID string `json:"createdByUserId"`

	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`

	TaskCount            int   `json:"taskCount"`
	EstimatedSeconds     int64 `json:"estimatedSeconds"`
	UnestimatedTaskCount int   `json:"unestimatedTaskCount"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type sprintReportTaskResponse struct {
	TaskID string `json:"taskId"`

	Title string `json:"title"`

	EstimateSeconds *int64 `json:"estimateSeconds"`

	Archived  bool `json:"archived"`
	Completed bool `json:"completed"`

	CarriedOver bool `json:"carriedOver"`

	CarriedOverToSprintID *string `json:"carriedOverToSprintId,omitempty"`
}

type sprintReportResponse struct {
	SprintID string `json:"sprintId"`

	State string `json:"state"`

	Frozen bool `json:"frozen"`

	CommitmentTaskCount        int   `json:"commitmentTaskCount"`
	CommitmentEstimatedSeconds int64 `json:"commitmentEstimatedSeconds"`
	CommitmentUnestimatedCount int   `json:"commitmentUnestimatedCount"`

	AddedTaskCount        int   `json:"addedTaskCount"`
	AddedEstimatedSeconds int64 `json:"addedEstimatedSeconds"`

	RemovedTaskCount        int   `json:"removedTaskCount"`
	RemovedEstimatedSeconds int64 `json:"removedEstimatedSeconds"`

	CompletedTaskCount        int   `json:"completedTaskCount"`
	CompletedEstimatedSeconds int64 `json:"completedEstimatedSeconds"`

	CarryoverTaskCount        int   `json:"carryoverTaskCount"`
	CarryoverEstimatedSeconds int64 `json:"carryoverEstimatedSeconds"`

	ArchivedTaskCount int `json:"archivedTaskCount"`

	UnestimatedTaskCount int `json:"unestimatedTaskCount"`

	Tasks []sprintReportTaskResponse `json:"tasks"`

	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

func toSprintResponse(item Sprint) sprintResponse {
	return sprintResponse{
		ID:        item.ID,
		ProjectID: item.ProjectID,

		Name: item.Name,
		Goal: item.Goal,

		PlannedStartAt: item.PlannedStartAt,
		PlannedEndAt:   item.PlannedEndAt,

		State: string(item.State),

		CreatedByUserID: item.CreatedByUserID,

		StartedAt:   item.StartedAt,
		CompletedAt: item.CompletedAt,

		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func toSprintDetailResponse(
	item SprintDetail,
) sprintResponse {
	response := toSprintResponse(item.Sprint)

	response.TaskCount = item.Totals.TaskCount
	response.EstimatedSeconds = item.Totals.EstimatedSeconds
	response.UnestimatedTaskCount = item.Totals.UnestimatedTaskCount

	return response
}

func toReportResponse(report Report) sprintReportResponse {
	tasks := make(
		[]sprintReportTaskResponse,
		0,
		len(report.Tasks),
	)

	for _, task := range report.Tasks {
		tasks = append(tasks, sprintReportTaskResponse{
			TaskID: task.TaskID,

			Title: task.Title,

			EstimateSeconds: task.EstimateSeconds,

			Archived:  task.Archived,
			Completed: task.Completed,

			CarriedOver: task.CarriedOver,

			CarriedOverToSprintID: task.CarriedOverToSprintID,
		})
	}

	return sprintReportResponse{
		SprintID: report.SprintID,

		State: string(report.State),

		Frozen: report.Frozen,

		CommitmentTaskCount:        report.CommitmentTaskCount,
		CommitmentEstimatedSeconds: report.CommitmentEstimatedSeconds,
		CommitmentUnestimatedCount: report.CommitmentUnestimatedCount,

		AddedTaskCount:        report.AddedTaskCount,
		AddedEstimatedSeconds: report.AddedEstimatedSeconds,

		RemovedTaskCount:        report.RemovedTaskCount,
		RemovedEstimatedSeconds: report.RemovedEstimatedSeconds,

		CompletedTaskCount:        report.CompletedTaskCount,
		CompletedEstimatedSeconds: report.CompletedEstimatedSeconds,

		CarryoverTaskCount:        report.CarryoverTaskCount,
		CarryoverEstimatedSeconds: report.CarryoverEstimatedSeconds,

		ArchivedTaskCount: report.ArchivedTaskCount,

		UnestimatedTaskCount: report.UnestimatedTaskCount,

		Tasks: tasks,

		CompletedAt: report.CompletedAt,
	}
}

// -------------------------------------------------------------------------
// Handlers
// -------------------------------------------------------------------------

func (h *Handler) List(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	limit := 0

	if raw := r.URL.Query().Get("limit"); raw != "" {
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

	var state *State

	if raw := r.URL.Query().Get("state"); raw != "" {
		value := State(raw)

		switch value {
		case StatePlanned, StateActive, StateCompleted:
			state = &value
		default:
			http.Error(
				w,
				"invalid state",
				http.StatusBadRequest,
			)
			return
		}
	}

	cursor := r.URL.Query().Get("cursor")

	if cursor != "" {
		if _, err := DecodeCursor(cursor); err != nil {
			http.Error(
				w,
				"invalid cursor",
				http.StatusBadRequest,
			)
			return
		}
	}

	page, err := h.service.List(
		r.Context(),
		ListInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),

			State: state,

			Limit: limit,

			Cursor: cursor,
		},
	)
	if err != nil {
		h.writeError(w, "list sprints", err)
		return
	}

	items := make([]sprintResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toSprintResponse(item))
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		listSprintsResponse{
			Items:      items,
			NextCursor: page.NextCursor,
		},
	); err != nil {
		log.Printf("encode sprint list: %v", err)
	}
}

func (h *Handler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	var req createSprintRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	sprint, err := h.service.Create(
		r.Context(),
		CreateInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),

			Name: req.Name,
			Goal: req.Goal,

			PlannedStartAt: req.PlannedStartAt,
			PlannedEndAt:   req.PlannedEndAt,
		},
	)
	if err != nil {
		h.writeError(w, "create sprint", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(
		toSprintResponse(sprint),
	); err != nil {
		log.Printf("encode sprint: %v", err)
	}
}

func (h *Handler) Get(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	detail, err := h.service.Get(
		r.Context(),
		GetInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),
			SprintID:  chi.URLParam(r, "sprintID"),
		},
	)
	if err != nil {
		h.writeError(w, "get sprint", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		toSprintDetailResponse(detail),
	); err != nil {
		log.Printf("encode sprint: %v", err)
	}
}

func (h *Handler) Update(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	var req updateSprintRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	input := UpdateInput{
		Identity: identity,

		ProjectID: chi.URLParam(r, "projectID"),
		SprintID:  chi.URLParam(r, "sprintID"),

		Name: req.Name,

		PlannedEndAt: req.PlannedEndAt,
	}

	// goal: absent = leave alone, null = clear, string = replace.
	if len(req.Goal) > 0 {
		input.GoalIsSet = true

		if string(req.Goal) != "null" {
			var goal string

			if err := json.Unmarshal(req.Goal, &goal); err != nil {
				http.Error(
					w,
					"goal must be a string or null",
					http.StatusBadRequest,
				)
				return
			}

			input.Goal = &goal
		}
	}

	// plannedStartAt: absent = leave alone, null = clear, date = replace.
	if len(req.PlannedStartAt) > 0 {
		input.PlannedStartIsSet = true

		if string(req.PlannedStartAt) != "null" {
			var value time.Time

			if err := json.Unmarshal(
				req.PlannedStartAt,
				&value,
			); err != nil {
				http.Error(
					w,
					"plannedStartAt must be a date or null",
					http.StatusBadRequest,
				)
				return
			}

			input.PlannedStartAt = &value
		}
	}

	sprint, err := h.service.Update(r.Context(), input)
	if err != nil {
		h.writeError(w, "update sprint", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		toSprintResponse(sprint),
	); err != nil {
		log.Printf("encode sprint: %v", err)
	}
}

func (h *Handler) Delete(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	if err := h.service.Delete(
		r.Context(),
		DeleteInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),
			SprintID:  chi.URLParam(r, "sprintID"),
		},
	); err != nil {
		h.writeError(w, "delete sprint", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Start(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	sprint, err := h.service.Start(
		r.Context(),
		StartInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),
			SprintID:  chi.URLParam(r, "sprintID"),
		},
	)
	if err != nil {
		h.writeError(w, "start sprint", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		toSprintResponse(sprint),
	); err != nil {
		log.Printf("encode sprint: %v", err)
	}
}

func (h *Handler) Complete(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	var req completeSprintRequest

	// The body is optional: an empty body means backlog carryover.
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			http.Error(
				w,
				httpx.DecodeErrorMessage(err),
				httpx.DecodeErrorStatus(err),
			)
			return
		}
	}

	sprint, report, err := h.service.Complete(
		r.Context(),
		CompleteInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),
			SprintID:  chi.URLParam(r, "sprintID"),

			CarryoverSprintID: req.CarryoverSprintID,
		},
	)
	if err != nil {
		h.writeError(w, "complete sprint", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		map[string]any{
			"sprint": toSprintResponse(sprint),
			"report": toReportResponse(report),
		},
	); err != nil {
		log.Printf("encode sprint completion: %v", err)
	}
}

func (h *Handler) Report(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	report, err := h.service.Report(
		r.Context(),
		ReportInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),
			SprintID:  chi.URLParam(r, "sprintID"),
		},
	)
	if err != nil {
		h.writeError(w, "sprint report", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		toReportResponse(report),
	); err != nil {
		log.Printf("encode sprint report: %v", err)
	}
}

// SetTaskSprint assigns a task to a sprint, moves it to another sprint or
// returns it to the backlog.
func (h *Handler) SetTaskSprint(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	var req setTaskSprintRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	if req.SprintID != nil {
		if _, err := uuid.Parse(*req.SprintID); err != nil {
			http.Error(
				w,
				"invalid sprint id",
				http.StatusBadRequest,
			)
			return
		}
	}

	task, err := h.service.SetTaskSprint(
		r.Context(),
		SetTaskSprintInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),
			TaskID:    chi.URLParam(r, "taskID"),

			SprintID: req.SprintID,
		},
	)
	if err != nil {
		h.writeError(w, "set task sprint", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		map[string]any{
			"taskId":   task.ID,
			"sprintId": task.SprintID,
		},
	); err != nil {
		log.Printf("encode task sprint: %v", err)
	}
}

// -------------------------------------------------------------------------
// Errors
// -------------------------------------------------------------------------

func (h *Handler) writeError(
	w http.ResponseWriter,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, ErrInvalidID),
		errors.Is(err, project.ErrInvalidID),
		errors.Is(err, ErrInvalidName),
		errors.Is(err, ErrInvalidGoal),
		errors.Is(err, ErrInvalidDateRange),
		errors.Is(err, ErrInvalidState),
		errors.Is(err, ErrInvalidCursor),
		errors.Is(err, ErrCarryoverTargetInvalid):

		http.Error(w, err.Error(), http.StatusBadRequest)

	case errors.Is(err, ErrSprintNotFound),
		errors.Is(err, ErrTaskNotFound),
		errors.Is(err, project.ErrNotFound):

		http.Error(w, err.Error(), http.StatusNotFound)

	case errors.Is(err, ErrForbidden),
		errors.Is(err, project.ErrForbidden):

		http.Error(w, err.Error(), http.StatusForbidden)

	case errors.Is(err, ErrLifecycleConflict),
		errors.Is(err, ErrActiveSprintExists),
		errors.Is(err, ErrTaskNotEligible):

		http.Error(w, err.Error(), http.StatusConflict)

	default:
		log.Printf("%s: %v", action, err)
		http.Error(
			w,
			"failed to "+action,
			http.StatusInternalServerError,
		)
	}
}
