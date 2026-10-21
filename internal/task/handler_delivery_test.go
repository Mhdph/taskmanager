package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

func TestDeliveryEndpointsRequireIdentity(t *testing.T) {
	h := NewHandler(nil)
	for _, handler := range []http.HandlerFunc{h.SubmitDelivery, h.ReviewDelivery, h.WithdrawDelivery, h.GetDelivery, h.ListDeliveries} {
		response := httptest.NewRecorder()
		handler(response, httptest.NewRequest("POST", "/", nil))
		if response.Code != 401 {
			t.Fatalf("unauthenticated request returned %d", response.Code)
		}
	}
}

func TestDeliveryHandlersRejectInvalidInputBeforeDatabase(t *testing.T) {
	h := NewHandler(&Service{})
	for _, test := range []struct {
		handler     http.HandlerFunc
		body, query string
	}{
		{h.SubmitDelivery, `{"body":"output","reviewerUserId":"bob","submittedByUserId":"mallory"}`, ""},
		{h.SubmitDelivery, `{"body":"output","reviewerUserId":"alice"}`, ""},
		{h.SubmitDelivery, `{"body":" ","reviewerUserId":"bob"}`, ""},
		{h.SubmitDelivery, `{"body":"output","reviewerUserId":"bob","outputUrl":"javascript:alert(1)"}`, ""},
		{h.SubmitDelivery, `{}`, ""},
		{h.SubmitDelivery, `{} {}`, ""},
		{h.ReviewDelivery, `{"decision":"changes_requested","note":" "}`, ""},
		{h.ReviewDelivery, `{"decision":"withdrawn"}`, ""},
		{h.ReviewDelivery, `{"decision":"approved","reviewedByUserId":"bob"}`, ""},
		{h.WithdrawDelivery, `{"decision":"approved"}`, ""},
		{h.ListDeliveries, "", "?limit=0"},
		{h.ListDeliveries, "", "?limit=101"},
		{h.ListDeliveries, "", "?limit=invalid"},
		{h.ListDeliveries, "", "?cursor=invalid"},
		{h.ListDeliveries, "", "?cursor=-1"},
	} {
		route := chi.NewRouteContext()
		route.URLParams.Add("projectID", uuid.NewString())
		route.URLParams.Add("taskID", uuid.NewString())
		route.URLParams.Add("deliveryID", uuid.NewString())
		ctx := context.WithValue(auth.WithIdentity(context.Background(), auth.Identity{UserID: "alice", TenantID: "a"}), chi.RouteCtxKey, route)
		request := httptest.NewRequest("POST", "/"+test.query, strings.NewReader(test.body)).WithContext(ctx)
		response := httptest.NewRecorder()
		test.handler(response, request)
		if response.Code != 400 {
			t.Fatalf("%s %s returned %d", test.body, test.query, response.Code)
		}
	}
}

func TestDeliveryErrorResponses(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{ErrDeliveryPending, 409}, {ErrDeliveryResolved, 409}, {ErrInvalidDeliveryReviewer, 400},
		{ErrInvalidDeliveryReview, 400}, {ErrDeliveryNotFound, 404}, {ErrTaskNotFound, 404},
		{project.ErrForbidden, 403}, {errors.New("private database error"), 500},
	} {
		response := httptest.NewRecorder()
		writeDeliveryResult(response, 200, nil, test.err)
		if response.Code != test.status {
			t.Fatalf("%v: %d", test.err, response.Code)
		}
		if strings.Contains(response.Body.String(), "private database error") {
			t.Fatal("database error exposed")
		}
	}
}
