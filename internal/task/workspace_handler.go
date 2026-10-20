package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/project"
)

// -------------------------------------------------------------------------
// Workspace task list
// -------------------------------------------------------------------------

type workspaceTaskResponse struct {
	taskResponse

	StatusName     string `json:"statusName"`
	StatusCategory string `json:"statusCategory"`
	ProjectName    string `json:"projectName"`
}

type workspaceListResponse struct {
	Items []workspaceTaskResponse `json:"items"`

	NextCursor *string `json:"nextCursor"`
}

func toWorkspaceTaskResponse(
	task WorkspaceTask,
) workspaceTaskResponse {
	return workspaceTaskResponse{
		taskResponse: toTaskResponse(task.Task),

		StatusName:     task.StatusName,
		StatusCategory: task.StatusCategory,
		ProjectName:    task.ProjectName,
	}
}

func (h *Handler) WorkspaceList(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)
		return
	}

	query := r.URL.Query()

	input := WorkspaceListInput{
		Identity: identity,

		ProjectID:      query.Get("project"),
		Assignee:       query.Get("assignee"),
		Priority:       query.Get("priority"),
		Status:         query.Get("status"),
		StatusCategory: query.Get("category"),
		ParentTaskID:   query.Get("parent"),

		Sort: query.Get("sort"),

		// Default is newest first; `dir=asc` flips it.
		Desc: query.Get("dir") != "asc",

		Cursor: query.Get("cursor"),

		Search: query.Get("search"),
	}

	// "q" is the query-string convention used by the UI's search-in-view.
	if input.Search == "" {
		input.Search = query.Get("q")
	}

	var parseErr error

	if raw := query.Get("archived"); raw != "" {
		input.Archived, parseErr = strconv.ParseBool(raw)
		if parseErr != nil {
			http.Error(w, "invalid archived filter", http.StatusBadRequest)
			return
		}
	}

	if raw := query.Get("overdue"); raw != "" {
		input.OverdueOnly, parseErr = strconv.ParseBool(raw)
		if parseErr != nil {
			http.Error(w, "invalid overdue filter", http.StatusBadRequest)
			return
		}
	}

	if raw := query.Get("completed"); raw != "" {
		input.CompletedOnly, parseErr = strconv.ParseBool(raw)
		if parseErr != nil {
			http.Error(w, "invalid completed filter", http.StatusBadRequest)
			return
		}
	}

	sprintID, backlogOnly, ok := parseSprintFilters(query)
	if !ok {
		http.Error(
			w,
			"invalid sprint filter",
			http.StatusBadRequest,
		)
		return
	}

	input.SprintID = sprintID
	input.BacklogOnly = backlogOnly

	parseTimeParam := func(
		names ...string,
	) (*time.Time, error) {
		for _, name := range names {
			raw := query.Get(name)

			if raw == "" {
				continue
			}

			value, err := time.Parse(
				time.RFC3339,
				raw,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid %s filter",
					name,
				)
			}

			return &value, nil
		}

		return nil, nil
	}

	var err error

	if input.DueFrom, err = parseTimeParam("dueFrom"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if input.DueTo, err = parseTimeParam("dueTo"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if input.StartFrom, err = parseTimeParam("startFrom"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if input.StartTo, err = parseTimeParam("startTo"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if raw := query.Get("limit"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}

		input.Limit = parsed
	}

	page, err := h.service.WorkspaceList(
		r.Context(),
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidSort),
			errors.Is(err, ErrInvalidStatusCategory),
			errors.Is(err, ErrInvalidPriority),
			errors.Is(err, ErrInvalidCursor),
			errors.Is(err, project.ErrInvalidID),
			errors.Is(err, ErrInvalidTaskDateRange):

			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		default:
			log.Printf(
				"workspace task list: %v",
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
		[]workspaceTaskResponse,
		0,
		len(page.Items),
	)

	for _, item := range page.Items {
		items = append(
			items,
			toWorkspaceTaskResponse(item),
		)
	}

	response := workspaceListResponse{
		Items: items,

		NextCursor: page.NextCursor,
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode workspace tasks response: %v",
			err,
		)
	}
}

// -------------------------------------------------------------------------
// Bulk operations
// -------------------------------------------------------------------------

type bulkTasksRequest struct {
	TaskIDs []string `json:"taskIds"`

	Status   *string `json:"status"`
	Priority *string `json:"priority"`

	// "none" unassigns the tasks.
	Assignee *string `json:"assignee"`

	Archive bool `json:"archive"`
}

type bulkTaskResultItemResponse struct {
	TaskID string `json:"taskId"`

	OK bool `json:"ok"`

	Error string `json:"error,omitempty"`
}

type bulkTasksResponse struct {
	Results []bulkTaskResultItemResponse `json:"results"`

	Updated int `json:"updated"`
}

func (h *Handler) Bulk(
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

	var req bulkTasksRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	input := BulkUpdateInput{
		Identity: identity,
		TaskIDs:  req.TaskIDs,
		Archive:  req.Archive,
	}

	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)

		if status != "" {
			input.Status = &status
		}
	}

	if req.Priority != nil {
		priority := Priority(*req.Priority)
		input.Priority = &priority
	}

	if req.Assignee != nil {
		assignee := strings.TrimSpace(*req.Assignee)

		if assignee != "" {
			input.Assignee = &assignee
		}
	}

	result, err := h.service.BulkUpdate(
		r.Context(),
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrNoChanges),
			errors.Is(err, ErrInvalidPriority),
			errors.Is(err, ErrInvalidID):

			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		default:
			log.Printf(
				"bulk update tasks: %v",
				err,
			)

			http.Error(
				w,
				"failed to bulk update tasks",
				http.StatusInternalServerError,
			)
		}

		return
	}

	results := make(
		[]bulkTaskResultItemResponse,
		0,
		len(result.Results),
	)

	for _, item := range result.Results {
		results = append(
			results,
			bulkTaskResultItemResponse{
				TaskID: item.TaskID,

				OK:    item.OK,
				Error: item.Err,
			},
		)
	}

	response := bulkTasksResponse{
		Results: results,
		Updated: result.Updated,
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode bulk tasks response: %v",
			err,
		)
	}
}
