package comment

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
	"github.com/mhdph/taskmanager/internal/task"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

type createCommentRequest struct {
	Body string `json:"body"`

	MentionedUserIDs []string `json:"mentionedUserIds"`
}
type commentResponse struct {
	ID string `json:"id"`

	TaskID string `json:"taskId"`

	AuthorUserID string `json:"authorUserId"`

	Body string `json:"body"`

	MentionedUserIDs []string `json:"mentionedUserIds"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type listCommentsResponse struct {
	Items      []commentResponse `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

func toCommentResponse(
	comment Comment,
) commentResponse {
	return commentResponse{
		ID: comment.ID,

		TaskID: comment.TaskID,

		AuthorUserID: comment.AuthorUserID,

		Body: comment.Body,

		MentionedUserIDs: comment.MentionedUserIDs,

		CreatedAt: comment.CreatedAt,
		UpdatedAt: comment.UpdatedAt,
	}
}

func (h *Handler) Create(
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

	projectID :=
		chi.URLParam(
			r,
			"projectID",
		)

	taskID :=
		chi.URLParam(
			r,
			"taskID",
		)

	var req createCommentRequest

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

	created, err :=
		h.service.Create(
			r.Context(),
			CreateInput{
				Identity: identity,

				ProjectID: projectID,

				TaskID: taskID,

				Body: req.Body,

				MentionedUserIDs: req.MentionedUserIDs,
			},
		)

	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrInvalidBody,
		):
			http.Error(
				w,
				"invalid comment body",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrMentionedUserNotMember,
		):
			http.Error(
				w,
				"mentioned user is not a project member",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			task.ErrTaskNotFound,
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
				"create comment: %v",
				err,
			)

			http.Error(
				w,
				"failed to create comment",
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
			toCommentResponse(
				created,
			),
		); err != nil {

		log.Printf(
			"encode comment: %v",
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
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = value
	}

	page, err := h.service.List(
		r.Context(),
		ListInput{
			Identity:  identity,
			ProjectID: chi.URLParam(r, "projectID"),
			TaskID:    chi.URLParam(r, "taskID"),
			Limit:     limit,
			Cursor:    r.URL.Query().Get("cursor"),
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID):
			http.Error(w, "invalid id or cursor", http.StatusBadRequest)
		case errors.Is(err, task.ErrTaskNotFound):
			http.Error(w, "task not found", http.StatusNotFound)
		case errors.Is(err, project.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			log.Printf("list comments: %v", err)
			http.Error(w, "failed to list comments", http.StatusInternalServerError)
		}
		return
	}

	items := make([]commentResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toCommentResponse(item))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(listCommentsResponse{
		Items:      items,
		NextCursor: page.NextCursor,
	}); err != nil {
		log.Printf("encode comment list: %v", err)
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

	err := h.service.Delete(
		r.Context(),
		identity,
		chi.URLParam(r, "projectID"),
		chi.URLParam(r, "taskID"),
		chi.URLParam(r, "commentID"),
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID):
			http.Error(w, "invalid id", http.StatusBadRequest)
		case errors.Is(err, ErrCommentNotFound):
			http.Error(w, "comment not found", http.StatusNotFound)
		case errors.Is(err, task.ErrTaskNotFound):
			http.Error(w, "task not found", http.StatusNotFound)
		case errors.Is(err, project.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			log.Printf("delete comment: %v", err)
			http.Error(w, "failed to delete comment", http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
