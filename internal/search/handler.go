package search

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/mhdph/taskmanager/internal/auth"
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

type resultResponse struct {
	Type string `json:"type"`

	ID string `json:"id"`

	ProjectID *string `json:"projectId,omitempty"`

	Title string `json:"title"`

	Description *string `json:"description,omitempty"`
}

type searchResponse struct {
	Projects []resultResponse `json:"projects"`

	Tasks []resultResponse `json:"tasks"`
}

func toResultResponse(
	item Result,
) resultResponse {
	return resultResponse{
		Type: string(item.Type),

		ID: item.ID,

		ProjectID: item.ProjectID,

		Title: item.Title,

		Description: item.Description,
	}
}

func (h *Handler) Search(
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

	query :=
		r.URL.Query()

	limit := 20

	if raw :=
		query.Get("limit"); raw != "" {

		value, err :=
			strconv.Atoi(raw)

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

	result, err :=
		h.service.Search(
			r.Context(),
			Input{
				Identity: identity,

				Query: query.Get("q"),

				Type: query.Get("type"),

				Limit: limit,
			},
		)

	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrInvalidQuery,
		):
			http.Error(
				w,
				"search query must contain at least 2 characters",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrInvalidType,
		):
			http.Error(
				w,
				"invalid search type",
				http.StatusBadRequest,
			)

		default:
			log.Printf(
				"search: %v",
				err,
			)

			http.Error(
				w,
				"search failed",
				http.StatusInternalServerError,
			)
		}

		return
	}

	projects := make(
		[]resultResponse,
		0,
		len(result.Projects),
	)

	for _, item := range result.Projects {

		projects = append(
			projects,
			toResultResponse(item),
		)
	}

	tasks := make(
		[]resultResponse,
		0,
		len(result.Tasks),
	)

	for _, item := range result.Tasks {

		tasks = append(
			tasks,
			toResultResponse(item),
		)
	}

	response :=
		searchResponse{
			Projects: projects,

			Tasks: tasks,
		}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).
		Encode(response); err != nil {

		log.Printf(
			"encode search response: %v",
			err,
		)
	}
}
