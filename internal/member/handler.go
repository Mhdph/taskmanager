package member

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

const defaultListLimit = 500

type memberResponse struct {
	UserID    string `json:"userId"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`

	Role   string `json:"role"`
	Title  string `json:"title"`
	Status string `json:"status"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type listMembersResponse struct {
	Items []memberResponse `json:"items"`
}

type updateMemberRequest struct {
	Role  *string `json:"role"`
	Title *string `json:"title"`
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}

func toMemberResponse(member Member) memberResponse {
	return memberResponse{
		UserID:    member.UserID,
		Email:     member.Email,
		FirstName: member.FirstName,
		LastName:  member.LastName,

		Role:   string(member.Role),
		Title:  member.Title,
		Status: string(member.Status),

		CreatedAt: formatTime(member.CreatedAt),
		UpdatedAt: formatTime(member.UpdatedAt),
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

	options := ListOptions{
		Limit: defaultListLimit,
	}

	if raw := r.URL.Query().Get("role"); raw != "" {
		role := Role(raw)

		if !role.Valid() {
			http.Error(
				w,
				"invalid role filter",
				http.StatusBadRequest,
			)
			return
		}

		options.Role = &role
	}

	if raw := r.URL.Query().Get("status"); raw != "" {
		status := Status(raw)

		if !status.Valid() {
			http.Error(
				w,
				"invalid status filter",
				http.StatusBadRequest,
			)
			return
		}

		options.Status = &status
	}

	if raw := r.URL.Query().Get("q"); raw != "" {
		query := strings.TrimSpace(raw)
		options.Search = &query
	}

	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > defaultListLimit {
			http.Error(
				w,
				"invalid limit",
				http.StatusBadRequest,
			)
			return
		}

		options.Limit = int32(parsed)
	}

	members, err := h.service.List(
		r.Context(),
		identity,
		options,
	)
	if err != nil {
		log.Printf(
			"list members: %v",
			err,
		)

		http.Error(
			w,
			"failed to list members",
			http.StatusInternalServerError,
		)

		return
	}

	items := make(
		[]memberResponse,
		0,
		len(members),
	)

	for _, member := range members {
		items = append(
			items,
			toMemberResponse(member),
		)
	}

	response := listMembersResponse{
		Items: items,
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode members response: %v",
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

	userID := chi.URLParam(
		r,
		"userID",
	)

	if userID == "" {
		http.Error(
			w,
			"user id is required",
			http.StatusBadRequest,
		)
		return
	}

	member, err := h.service.Get(
		r.Context(),
		identity,
		userID,
	)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(
				w,
				"member not found",
				http.StatusNotFound,
			)
			return
		}

		log.Printf(
			"get member: %v",
			err,
		)

		http.Error(
			w,
			"failed to get member",
			http.StatusInternalServerError,
		)

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		toMemberResponse(member),
	); err != nil {
		log.Printf(
			"encode member response: %v",
			err,
		)
	}
}

func (h *Handler) Update(
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

	userID := chi.URLParam(
		r,
		"userID",
	)

	if userID == "" {
		http.Error(
			w,
			"user id is required",
			http.StatusBadRequest,
		)
		return
	}

	var req updateMemberRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	if req.Role == nil && req.Title == nil {
		http.Error(
			w,
			"no changes provided",
			http.StatusBadRequest,
		)
		return
	}

	input := UpdateInput{
		Identity: identity,
		UserID:   userID,
	}

	if req.Role != nil {
		role := Role(*req.Role)
		input.Role = &role
	}

	if req.Title != nil {
		title := *req.Title

		if title == "" {
			input.ClearTitle = true
		}

		input.Title = &title
	}

	member, err := h.service.Update(
		r.Context(),
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRole):
			http.Error(
				w,
				"invalid member role",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrLastOwner):
			http.Error(
				w,
				"workspace must have at least one owner",
				http.StatusConflict,
			)

		case errors.Is(err, ErrNotFound):
			http.Error(
				w,
				"member not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"update member: %v",
				err,
			)

			http.Error(
				w,
				"failed to update member",
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
		toMemberResponse(member),
	); err != nil {
		log.Printf(
			"encode member response: %v",
			err,
		)
	}
}

func (h *Handler) Remove(
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

	userID := chi.URLParam(
		r,
		"userID",
	)

	if userID == "" {
		http.Error(
			w,
			"user id is required",
			http.StatusBadRequest,
		)
		return
	}

	err := h.service.Remove(
		r.Context(),
		identity,
		userID,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrLastOwner):
			http.Error(
				w,
				"workspace must have at least one owner",
				http.StatusConflict,
			)

		case errors.Is(err, ErrNotFound):
			http.Error(
				w,
				"member not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"remove member: %v",
				err,
			)

			http.Error(
				w,
				"failed to remove member",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
