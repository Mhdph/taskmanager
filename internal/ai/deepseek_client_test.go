package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type aiTransport func(*http.Request) (*http.Response, error)

func (f aiTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testDeepSeek(t *testing.T, status int, content, finish string) *DeepSeekClient {
	t.Helper()
	client, err := NewDeepSeekClient("test-key", "https://api.deepseek.com", "deepseek-flash", &http.Client{Transport: aiTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("invalid DeepSeek request")
		}
		var payload struct {
			Model          string `json:"model"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Thinking struct {
				Type string `json:"type"`
			} `json:"thinking"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Model != "deepseek-flash" || payload.ResponseFormat.Type != "json_object" || payload.Thinking.Type != "disabled" {
			t.Error("invalid DeepSeek model or output configuration")
		}
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": finish, "message": map[string]string{"content": content}}}})
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestDeepSeekOperations(t *testing.T) {
	ctx := context.Background()
	draft, err := testDeepSeek(t, 200, `{"title":"تست","description":null,"priority":"medium","dueAt":null,"actionItems":[]}`, "stop").TaskFromText(ctx, "یک تسک بساز")
	if err != nil || draft.Title != "تست" {
		t.Fatalf("draft: %+v %v", draft, err)
	}
	summary, err := testDeepSeek(t, 200, `{"summary":"summary","actionItems":[],"risks":[],"nextSteps":[]}`, "stop").SummarizeTask(ctx, TaskContext{})
	if err != nil || summary.Summary != "summary" {
		t.Fatalf("task summary: %+v %v", summary, err)
	}
	suggestion, err := testDeepSeek(t, 200, `{"priority":"high","statusCategory":null,"dueAt":null,"reasoning":"urgent work"}`, "stop").SuggestTask(ctx, TaskContext{})
	if err != nil || suggestion.Priority == nil || *suggestion.Priority != "high" {
		t.Fatalf("suggestion: %+v %v", suggestion, err)
	}
	projectSummary, err := testDeepSeek(t, 200, `{"summary":"project","highlights":[],"risks":[],"blockedTasks":[],"nextActions":[]}`, "stop").SummarizeProject(ctx, ProjectContext{})
	if err != nil || projectSummary.Summary != "project" {
		t.Fatalf("project summary: %+v %v", projectSummary, err)
	}
}

func TestDeepSeekRejectsInvalidResults(t *testing.T) {
	for _, tt := range []struct {
		name, content, finish string
		status                int
	}{
		{"missing fields", `{"title":"task"}`, "stop", 200},
		{"unknown field", `{"title":"task","description":null,"priority":"medium","dueAt":null,"actionItems":[],"extra":true}`, "stop", 200},
		{"bad priority", `{"title":"task","description":null,"priority":"extreme","dueAt":null,"actionItems":[]}`, "stop", 200},
		{"bad date", `{"title":"task","description":null,"priority":"medium","dueAt":"tomorrow","actionItems":[]}`, "stop", 200},
		{"truncated output", `{}`, "length", 200},
		{"null output", `null`, "stop", 200},
		{"bad JSON", `oops`, "stop", 200},
		{"provider failure", `{}`, "stop", 503},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testDeepSeek(t, tt.status, tt.content, tt.finish).TaskFromText(context.Background(), "task")
			if err == nil {
				t.Fatal("invalid result accepted")
			}
			if tt.status == 503 && !errors.Is(err, ErrProviderUnavailable) {
				t.Fatalf("provider error: %v", err)
			}
		})
	}
}

func TestDeepSeekHonorsCancellation(t *testing.T) {
	client, err := NewDeepSeekClient("test-key", "https://api.deepseek.com", "deepseek-flash", &http.Client{Transport: aiTransport(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.TaskFromText(ctx, "task"); err == nil {
		t.Fatal("canceled request succeeded")
	}
}
