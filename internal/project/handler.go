package project

import (
	"encoding/json"
	"log"
	"net/http"

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
		http.Error(
			w,
			"failed to create project",
			http.StatusInternalServerError,
		)

		log.Printf("create project: %v", err)

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
