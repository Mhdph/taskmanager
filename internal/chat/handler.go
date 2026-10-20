package chat

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/storage"
)

type Handler struct {
	service *Service

	hub *Hub

	// allowedOrigins are the cross-origin hosts accepted by the WebSocket
	// upgrade. The request host is always accepted.
	allowedOrigins []string

	typingLimiter *typingLimiter
}

func NewHandler(
	service *Service,
	hub *Hub,
	allowedOrigins []string,
) *Handler {
	return &Handler{
		service: service,

		hub: hub,

		allowedOrigins: allowedOrigins,

		typingLimiter: newTypingLimiter(),
	}
}

// -------------------------------------------------------------------------
// Responses
// -------------------------------------------------------------------------

type conversationResponse struct {
	ID string `json:"id"`

	Type string `json:"type"`

	ProjectID *string `json:"projectId"`

	Title string `json:"title"`

	CreatedByUserID string `json:"createdByUserId"`

	LastMessageSeq int64 `json:"lastMessageSeq"`

	LastMessageAt *time.Time `json:"lastMessageAt"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toConversationResponse(item Conversation) conversationResponse {
	return conversationResponse{
		ID: item.ID,

		Type: string(item.Type),

		ProjectID: item.ProjectID,

		Title: item.Title,

		CreatedByUserID: item.CreatedByUserID,

		LastMessageSeq: item.LastMessageSeq,

		LastMessageAt: item.LastMessageAt,

		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

type memberResponse struct {
	UserID string `json:"userId"`

	Role string `json:"role"`

	LastReadSeq int64 `json:"lastReadSeq"`

	JoinedAt time.Time `json:"joinedAt"`
}

func toMemberResponse(item Member) memberResponse {
	return memberResponse{
		UserID: item.UserID,

		Role: string(item.Role),

		LastReadSeq: item.LastReadSeq,

		JoinedAt: item.JoinedAt,
	}
}

type messagePreviewResponse struct {
	Kind string `json:"kind"`

	Body string `json:"body"`

	AuthorUserID *string `json:"authorUserId"`

	CreatedAt time.Time `json:"createdAt"`
}

type conversationSummaryResponse struct {
	conversationResponse

	MemberRole string `json:"memberRole"`

	LastReadSeq int64 `json:"lastReadSeq"`

	UnreadCount int64 `json:"unreadCount"`

	LastMessage *messagePreviewResponse `json:"lastMessage"`

	MemberIDs []string `json:"memberIds"`
}

func toConversationSummaryResponse(
	item ConversationSummary,
) conversationSummaryResponse {
	response := conversationSummaryResponse{
		conversationResponse: toConversationResponse(item.Conversation),

		MemberRole: string(item.MemberRole),

		LastReadSeq: item.LastReadSeq,

		UnreadCount: item.UnreadCount,

		MemberIDs: item.MemberIDs,
	}

	if response.MemberIDs == nil {
		response.MemberIDs = []string{}
	}

	if item.LastMessage != nil {
		response.LastMessage = &messagePreviewResponse{
			Kind: string(item.LastMessage.Kind),

			Body: item.LastMessage.Body,

			AuthorUserID: item.LastMessage.AuthorUserID,

			CreatedAt: item.LastMessage.CreatedAt,
		}
	}

	return response
}

type conversationDetailResponse struct {
	conversationResponse

	Members []memberResponse `json:"members"`
}

func toConversationDetailResponse(
	item ConversationDetail,
) conversationDetailResponse {
	members := make(
		[]memberResponse,
		0,
		len(item.Members),
	)

	for _, member := range item.Members {
		members = append(
			members,
			toMemberResponse(member),
		)
	}

	return conversationDetailResponse{
		conversationResponse: toConversationResponse(item.Conversation),

		Members: members,
	}
}

type listConversationsResponse struct {
	Items []conversationSummaryResponse `json:"items"`
}

type listMessagesResponse struct {
	Items []WireMessage `json:"items"`

	NextCursor *string `json:"nextCursor"`

	// NextAfter is only set on forward replay pages.
	NextAfter *int64 `json:"nextAfter,omitempty"`
}

// -------------------------------------------------------------------------
// Requests
// -------------------------------------------------------------------------

type createConversationRequest struct {
	// Type is "dm" or "group". Project conversations are reached through
	// GET /projects/{projectID}/chat.
	Type string `json:"type"`

	Title string `json:"title"`

	MemberUserIds []string `json:"memberUserIds"`

	MemberUserId string `json:"memberUserId"`
}

type markReadRequest struct {
	LastReadSeq int64 `json:"lastReadSeq"`
}

type sendMessageRequest struct {
	Body string `json:"body"`

	// ClientMessageID makes retries idempotent: resending with the same id
	// returns the original message instead of creating a duplicate.
	ClientMessageID *string `json:"clientMessageId"`

	// AttachmentIDs binds pending uploads to this message. The body may be
	// empty when attachments are present.
	AttachmentIDs []string `json:"attachmentIds"`
}

type editMessageRequest struct {
	Body string `json:"body"`
}

type addMemberRequest struct {
	UserID string `json:"userId"`
}

// -------------------------------------------------------------------------
// Handlers
// -------------------------------------------------------------------------

func (h *Handler) List(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	conversations, err := h.service.List(
		r.Context(),
		identity,
	)
	if err != nil {
		h.writeError(w, "list conversations", err)
		return
	}

	items := make(
		[]conversationSummaryResponse,
		0,
		len(conversations),
	)

	for _, conversation := range conversations {
		items = append(
			items,
			toConversationSummaryResponse(
				conversation,
			),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		listConversationsResponse{Items: items},
	); err != nil {
		log.Printf(
			"encode conversation list: %v",
			err,
		)
	}
}

func (h *Handler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	var req createConversationRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	var (
		detail ConversationDetail
		err    error
		status = http.StatusCreated
	)

	switch req.Type {
	case string(ConversationTypeDM):
		detail, err = h.service.CreateDM(
			r.Context(),
			CreateDMInput{
				Identity: identity,

				OtherUserID: req.MemberUserId,
			},
		)

		// A DM is unique per pair: a second call returns the existing
		// conversation instead of creating a new one.
		status = http.StatusOK

	case string(ConversationTypeGroup):
		detail, err = h.service.CreateGroup(
			r.Context(),
			CreateGroupInput{
				Identity: identity,

				Title: req.Title,

				MemberIDs: req.MemberUserIds,
			},
		)

	default:
		http.Error(
			w,
			"type must be dm or group; use GET /projects/{projectID}/chat for a project chat",
			http.StatusBadRequest,
		)
		return
	}

	if err != nil {
		h.writeError(w, "create conversation", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(
		toConversationDetailResponse(detail),
	); err != nil {
		log.Printf(
			"encode conversation: %v",
			err,
		)
	}
}

func (h *Handler) Get(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	detail, err := h.service.Get(
		r.Context(),
		identity,
		chi.URLParam(r, "conversationID"),
	)
	if err != nil {
		h.writeError(w, "get conversation", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		toConversationDetailResponse(detail),
	); err != nil {
		log.Printf(
			"encode conversation detail: %v",
			err,
		)
	}
}

type presenceResponse struct {
	UserIDs []string `json:"userIds"`
}

// Presence returns the conversation members currently connected to this API
// instance.
func (h *Handler) Presence(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	detail, err := h.service.Get(
		r.Context(),
		identity,
		chi.URLParam(r, "conversationID"),
	)
	if err != nil {
		h.writeError(w, "conversation presence", err)
		return
	}

	userIDs := make([]string, 0, len(detail.Members))

	for _, member := range detail.Members {
		if h.hub.IsOnline(identity.TenantID, member.UserID) {
			userIDs = append(userIDs, member.UserID)
		}
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		presenceResponse{UserIDs: userIDs},
	); err != nil {
		log.Printf(
			"encode conversation presence: %v",
			err,
		)
	}
}

func (h *Handler) ListMessages(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
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

	page, err := h.service.ListMessages(
		r.Context(),
		ListMessagesInput{
			Identity: identity,

			ConversationID: chi.URLParam(r, "conversationID"),

			Before: r.URL.Query().Get("before"),

			After: r.URL.Query().Get("after"),

			Limit: limit,
		},
	)
	if err != nil {
		h.writeError(w, "list messages", err)
		return
	}

	items := make(
		[]WireMessage,
		0,
		len(page.Items),
	)

	for _, message := range page.Items {
		items = append(
			items,
			NewWireMessage(message),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		listMessagesResponse{
			Items:      items,
			NextCursor: page.NextCursor,

			NextAfter: page.NextAfter,
		},
	); err != nil {
		log.Printf(
			"encode message list: %v",
			err,
		)
	}
}

func (h *Handler) SendMessage(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	var req sendMessageRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	message, created, err := h.service.SendMessage(
		r.Context(),
		SendMessageInput{
			Identity: identity,

			ConversationID: chi.URLParam(r, "conversationID"),

			Body: req.Body,

			ClientMessageID: req.ClientMessageID,

			AttachmentIDs: req.AttachmentIDs,
		},
	)
	if err != nil {
		h.writeError(w, "send message", err)
		return
	}

	// 200 means the client message id was already stored and the original
	// message is being returned (idempotent retry). 201 means new.
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(
		NewWireMessage(message),
	); err != nil {
		log.Printf(
			"encode sent message: %v",
			err,
		)
	}
}

func (h *Handler) EditMessage(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	var req editMessageRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	message, err := h.service.EditMessage(
		r.Context(),
		EditMessageInput{
			Identity: identity,

			ConversationID: chi.URLParam(r, "conversationID"),

			MessageID: chi.URLParam(r, "messageID"),

			Body: req.Body,
		},
	)
	if err != nil {
		h.writeError(w, "edit message", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		NewWireMessage(message),
	); err != nil {
		log.Printf(
			"encode edited message: %v",
			err,
		)
	}
}

func (h *Handler) DeleteMessage(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	message, err := h.service.DeleteMessage(
		r.Context(),
		DeleteMessageInput{
			Identity: identity,

			ConversationID: chi.URLParam(r, "conversationID"),

			MessageID: chi.URLParam(r, "messageID"),
		},
	)
	if err != nil {
		h.writeError(w, "delete message", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		NewWireMessage(message),
	); err != nil {
		log.Printf(
			"encode deleted message: %v",
			err,
		)
	}
}

func (h *Handler) UploadAttachment(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		h.service.UploadRequestLimit(),
	)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError

		if errors.As(err, &maxErr) {
			http.Error(
				w,
				"request body too large",
				http.StatusRequestEntityTooLarge,
			)
			return
		}

		http.Error(
			w,
			"invalid multipart form",
			http.StatusBadRequest,
		)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(
			w,
			"file is required",
			http.StatusBadRequest,
		)
		return
	}
	defer file.Close()

	attachment, err := h.service.UploadAttachment(
		r.Context(),
		UploadAttachmentInput{
			Identity: identity,

			ConversationID: chi.URLParam(r, "conversationID"),

			Filename: header.Filename,

			ContentType: header.Header.Get("Content-Type"),

			Size: header.Size,

			Reader: file,
		},
	)
	if err != nil {
		h.writeError(w, "upload attachment", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(
		NewWireAttachment(attachment),
	); err != nil {
		log.Printf(
			"encode uploaded attachment: %v",
			err,
		)
	}
}

func (h *Handler) CancelAttachment(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	err := h.service.CancelAttachment(
		r.Context(),
		identity,
		chi.URLParam(r, "conversationID"),
		chi.URLParam(r, "attachmentID"),
	)
	if err != nil {
		h.writeError(w, "cancel attachment", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type attachmentDownloadResponse struct {
	URL string `json:"url"`

	ExpiresAt time.Time `json:"expiresAt"`
}

func (h *Handler) DownloadAttachment(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	download, err := h.service.GetAttachmentDownload(
		r.Context(),
		identity,
		chi.URLParam(r, "conversationID"),
		chi.URLParam(r, "attachmentID"),
	)
	if err != nil {
		h.writeError(w, "download attachment", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		attachmentDownloadResponse{
			URL: download.URL,

			ExpiresAt: download.ExpiresAt,
		},
	); err != nil {
		log.Printf(
			"encode attachment download: %v",
			err,
		)
	}
}

func (h *Handler) MarkRead(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	var req markReadRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	err := h.service.MarkRead(
		r.Context(),
		identity,
		chi.URLParam(r, "conversationID"),
		req.LastReadSeq,
	)
	if err != nil {
		h.writeError(w, "mark conversation read", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddMember(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	var req addMemberRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	err := h.service.AddMember(
		r.Context(),
		identity,
		chi.URLParam(r, "conversationID"),
		req.UserID,
	)
	if err != nil {
		h.writeError(w, "add conversation member", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveMember(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	err := h.service.RemoveMember(
		r.Context(),
		identity,
		chi.URLParam(r, "conversationID"),
		chi.URLParam(r, "userID"),
	)
	if err != nil {
		h.writeError(w, "remove conversation member", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ProjectChat serves GET /projects/{projectID}/chat, creating the project's
// conversation on first access.
func (h *Handler) ProjectChat(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	detail, err := h.service.ProjectChat(
		r.Context(),
		identity,
		chi.URLParam(r, "projectID"),
	)
	if err != nil {
		h.writeError(w, "get project chat", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		toConversationDetailResponse(detail),
	); err != nil {
		log.Printf(
			"encode project chat: %v",
			err,
		)
	}
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

func (h *Handler) identity(
	w http.ResponseWriter,
	r *http.Request,
) (auth.Identity, bool) {
	identity, ok := auth.IdentityFromContext(
		r.Context(),
	)
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)

		return auth.Identity{}, false
	}

	return identity, true
}

func (h *Handler) writeError(
	w http.ResponseWriter,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, ErrInvalidID),
		errors.Is(err, ErrInvalidCursor),
		errors.Is(err, ErrInvalidConversationType),
		errors.Is(err, ErrInvalidTitle),
		errors.Is(err, ErrInvalidMembers),
		errors.Is(err, ErrTooManyMembers),
		errors.Is(err, ErrInvalidReadCursor),
		errors.Is(err, ErrInvalidBody),
		errors.Is(err, ErrInvalidClientMessageID),
		errors.Is(err, ErrInvalidAttachment),
		errors.Is(err, ErrInvalidAttachmentFilename),
		errors.Is(err, ErrTooManyAttachments):

		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrConversationNotFound),
		errors.Is(err, ErrProjectNotFound),
		errors.Is(err, ErrMemberNotFound),
		errors.Is(err, ErrMessageNotFound),
		errors.Is(err, ErrAttachmentNotFound):

		http.Error(
			w,
			err.Error(),
			http.StatusNotFound,
		)

	case errors.Is(err, ErrForbidden),
		errors.Is(err, ErrNotConversationOwner),
		errors.Is(err, ErrCannotModifyMembers),
		errors.Is(err, ErrCannotRemoveSelf):

		http.Error(
			w,
			err.Error(),
			http.StatusForbidden,
		)

	case errors.Is(err, ErrAttachmentTooLarge):

		http.Error(
			w,
			err.Error(),
			http.StatusRequestEntityTooLarge,
		)

	case errors.Is(err, ErrAttachmentAttached):

		http.Error(
			w,
			err.Error(),
			http.StatusConflict,
		)

	case errors.Is(err, storage.ErrStorageDisabled):

		http.Error(
			w,
			"attachment storage is unavailable",
			http.StatusServiceUnavailable,
		)

	default:
		log.Printf(
			"%s: %v",
			action,
			err,
		)

		http.Error(
			w,
			"failed to "+action,
			http.StatusInternalServerError,
		)
	}
}
