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
)

func TestReminderEndpointsRequireIdentity(t *testing.T) {
	h := NewHandler(nil)
	for _, handler := range []http.HandlerFunc{h.CreateReminder, h.ListReminders, h.SnoozeReminder, h.CancelReminder} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/", nil))
		if w.Code != 401 {
			t.Fatalf("unauthenticated request returned %d", w.Code)
		}
	}
}

func TestReminderHandlersRejectBadRequests(t *testing.T) {
	h := NewHandler(&Service{})
	for _, tc := range []struct {
		handler     http.HandlerFunc
		body, query string
	}{
		{h.CreateReminder, `{}`, ""},
		{h.CreateReminder, `{"remindAt":"soon"}`, ""},
		{h.CreateReminder, `{"remindAt":"2020-01-01T00:00:00Z"}`, ""},
		{h.CreateReminder, `{"minutesBeforeDue":-1}`, ""},
		{h.CreateReminder, `{"minutesBeforeDue":1440,"userId":"bob"}`, ""},
		{h.CreateReminder, `{"remindAt":"2099-01-01T00:00:00Z","minutesBeforeDue":0}`, ""},
		{h.SnoozeReminder, `{}`, ""},
		{h.SnoozeReminder, `{"remindAt":"2020-01-01T00:00:00Z"}`, ""},
		{h.ListReminders, "", "?limit=0"}, {h.ListReminders, "", "?limit=101"},
		{h.ListReminders, "", "?cursor=invalid"},
	} {
		route := chi.NewRouteContext()
		route.URLParams.Add("projectID", uuid.NewString())
		route.URLParams.Add("taskID", uuid.NewString())
		route.URLParams.Add("reminderID", uuid.NewString())
		ctx := context.WithValue(auth.WithIdentity(context.Background(), auth.Identity{UserID: "alice", TenantID: "a"}), chi.RouteCtxKey, route)
		w := httptest.NewRecorder()
		tc.handler(w, httptest.NewRequest("POST", "/"+tc.query, strings.NewReader(tc.body)).WithContext(ctx))
		if w.Code != 400 {
			t.Fatalf("%s%s returned %d", tc.body, tc.query, w.Code)
		}
	}
}

func TestReminderErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{
		{ErrReminderNotFound, 404}, {ErrReminderCancelled, 409}, {ErrReminderLimit, 409}, {ErrInvalidReminder, 400}, {errors.New("db secret"), 500},
	} {
		w := httptest.NewRecorder()
		writeReminderResult(w, 200, nil, tc.err)
		if w.Code != tc.code || strings.Contains(w.Body.String(), "db secret") {
			t.Fatalf("%v => %d", tc.err, w.Code)
		}
	}
}
