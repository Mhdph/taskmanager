package project

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

type createProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type listProjectsResponse struct {
	Items      []projectResponse `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

type projectResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func (h *Handler) Create(
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

	var req createProjectRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	project, err := h.service.Create(
		r.Context(),
		CreateInput{
			TenantID:    identity.TenantID,
			UserID:      identity.UserID,
			Name:        req.Name,
			Description: req.Description,
		},
	)
	if err != nil {
		log.Printf(
			"create project: %v",
			err,
		)

		handleProjectError(
			w,
			"failed to create project",
			err,
		)

		return
	}

	response := projectResponse{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
		CreatedAt: project.CreatedAt.
			UTC().
			Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: project.UpdatedAt.
			UTC().
			Format("2006-01-02T15:04:05Z07:00"),
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Printf(
			"encode project response: %v",
			err,
		)
	}
}

func (h *Handler) List(
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

	limit := 20

	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			http.Error(
				w,
				"invalid limit",
				http.StatusBadRequest,
			)
			return
		}

		limit = parsed
	}

	cursor := r.URL.Query().Get("cursor")

	page, err := h.service.List(
		r.Context(),
		ListInput{
			Identity: identity,
			Limit:    limit,
			Cursor:   cursor,
		},
	)
	if err != nil {
		log.Printf(
			"list projects: %v",
			err,
		)

		handleProjectError(
			w,
			"failed to list projects",
			err,
		)

		return
	}

	items := make(
		[]projectResponse,
		0,
		len(page.Items),
	)

	for _, item := range page.Items {
		items = append(
			items,
			toProjectResponse(item),
		)
	}

	response := listProjectsResponse{
		Items:      items,
		NextCursor: page.NextCursor,
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode projects response: %v",
			err,
		)
	}
}

func (h *Handler) Get(
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

	projectID := chi.URLParam(
		r,
		"projectID",
	)

	if projectID == "" {
		http.Error(
			w,
			"project id is required",
			http.StatusBadRequest,
		)
		return
	}

	project, err := h.service.Get(
		r.Context(),
		identity,
		projectID,
	)
	if err != nil {
		log.Printf(
			"get project: %v",
			err,
		)

		handleProjectError(
			w,
			"failed to get project",
			err,
		)

		return
	}

	response := toProjectResponse(project)

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode project response: %v",
			err,
		)
	}
}

func handleProjectError(
	w http.ResponseWriter,
	message string,
	err error,
) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(
			w,
			ErrNotFound.Error(),
			http.StatusNotFound,
		)

	case errors.Is(err, ErrInvalidID),
		errors.Is(err, ErrNameRequired),
		errors.Is(err, ErrNameTooLong),
		errors.Is(err, ErrInvalidCursor):
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

	default:
		http.Error(
			w,
			message,
			http.StatusInternalServerError,
		)
	}
}

func toProjectResponse(project Project) projectResponse {
	return projectResponse{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
		CreatedAt: project.CreatedAt.
			UTC().
			Format(time.RFC3339),
		UpdatedAt: project.UpdatedAt.
			UTC().
			Format(time.RFC3339),
	}
}
