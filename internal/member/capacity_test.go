package member

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
)

func TestCapacityValidation(t *testing.T) {
	for _, input := range []CapacityInput{
		{UserID: "alice", DailyCapacitySeconds: -1, WorkingDays: []int16{1}, Timezone: "UTC"},
		{UserID: "alice", DailyCapacitySeconds: 86401, WorkingDays: []int16{1}, Timezone: "UTC"},
		{UserID: "alice", WorkingDays: nil, Timezone: "UTC"},
		{UserID: "alice", WorkingDays: []int16{1, 1}, Timezone: "UTC"},
		{UserID: "alice", WorkingDays: []int16{0}, Timezone: "UTC"},
		{UserID: "alice", WorkingDays: []int16{8}, Timezone: "UTC"},
		{UserID: "alice", WorkingDays: []int16{}, Timezone: "Local"},
		{UserID: "alice", WorkingDays: []int16{}, Timezone: "bad/timezone"},
	} {
		if _, err := normalizeCapacity(input); err == nil {
			t.Fatalf("invalid capacity accepted: %+v", input)
		}
	}
	input := CapacityInput{UserID: "alice", DailyCapacitySeconds: 28800, WorkingDays: []int16{6, 7, 1, 2, 3}, Timezone: "Asia/Tehran"}
	got, err := normalizeCapacity(input)
	if err != nil || got.WorkingDays[0] != 1 || input.WorkingDays[0] != 6 {
		t.Fatalf("normalization mutated input: %+v %v", got, err)
	}
	if _, err := normalizeCapacity(CapacityInput{UserID: "alice", WorkingDays: []int16{}, Timezone: "UTC"}); err != nil {
		t.Fatal("zero availability rejected")
	}
}

func TestWorkloadValidation(t *testing.T) {
	for _, input := range []WorkloadInput{
		{From: "2026-10-23", To: "2026-10-22", Limit: 50},
		{From: "2026-01-01", To: "2026-05-01", Limit: 50},
		{From: "", To: "2026-10-23", Limit: 50},
		{From: "0000-01-01", To: "0000-01-01", Limit: 50},
		{From: "2026-10-23", To: "2026-10-23", Limit: 201},
		{From: "2026-10-23", To: "2026-10-23", Limit: 50, Timezone: "Local"},
	} {
		if _, _, _, err := normalizeWorkload(input); err == nil {
			t.Fatalf("invalid workload accepted: %+v", input)
		}
	}
	if input, _, _, err := normalizeWorkload(WorkloadInput{From: "2026-10-23", To: "2026-10-23", Limit: 50}); err != nil || input.Timezone != "Asia/Tehran" {
		t.Fatal("valid range rejected")
	}
}

func TestCapacityHandlersAuthenticationAndMalformedInput(t *testing.T) {
	h := NewHandler(&Service{})
	for _, handler := range []http.HandlerFunc{h.GetCapacity, h.SetCapacity, h.ClearCapacity, h.Workload} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 401 {
			t.Fatalf("missing identity returned %d", w.Code)
		}
	}
	for _, tc := range []struct {
		handler    http.HandlerFunc
		body, path string
	}{
		{h.SetCapacity, `{"workingDays":[1],"timezone":"UTC"}`, "/"},
		{h.SetCapacity, `{"dailyCapacitySeconds":-1,"workingDays":[1],"timezone":"UTC"}`, "/"},
		{h.SetCapacity, `{"dailyCapacitySeconds":1,"workingDays":[1,1],"timezone":"UTC"}`, "/"},
		{h.SetCapacity, `{"dailyCapacitySeconds":1,"workingDays":[1],"timezone":"UTC","userId":"bob"}`, "/"},
		{h.Workload, "", "/?from=2026-10-23&to=2026-10-22"},
		{h.Workload, "", "/?limit=abc"},
	} {
		route := chi.NewRouteContext()
		route.URLParams.Add("userID", "alice")
		ctx := context.WithValue(auth.WithIdentity(context.Background(), auth.Identity{UserID: "admin", TenantID: "a"}), chi.RouteCtxKey, route)
		w := httptest.NewRecorder()
		tc.handler(w, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)).WithContext(ctx))
		if w.Code != 400 {
			t.Fatalf("%s%s: %d", tc.path, tc.body, w.Code)
		}
	}
}
