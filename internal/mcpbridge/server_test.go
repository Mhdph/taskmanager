package mcpbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const projectID = "11111111-1111-4111-8111-111111111111"
const taskID = "22222222-2222-4222-8222-222222222222"
const statusID = "33333333-3333-4333-8333-333333333333"

func connectTest(t *testing.T, handler http.HandlerFunc) *mcp.ClientSession {
	t.Helper()
	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)
	c, err := NewClient(Config{APIURL: api.URL, APIKey: "secret-token", UserID: "alice", AppID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := NewServer(c).Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestMCPWorkflow(t *testing.T) {
	type request struct {
		method, path, query string
		body                map[string]any
	}
	requests := make(chan request, 20)
	cs := connectTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-APIKEY") != "secret-token" || r.Header.Get("USER-ID") != "alice" || r.Header.Get("APP-ID") != "tenant-a" {
			t.Error("missing authenticated identity headers")
		}
		var body map[string]any
		if r.Method != "GET" {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		requests <- request{r.Method, r.URL.Path, r.URL.RawQuery, body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"nextCursor":"next-page"}`))
	})
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 12 {
		t.Fatalf("got %d tools, want 12", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		if tool.InputSchema == nil || tool.Annotations == nil {
			t.Fatalf("missing schema or annotations: %s", tool.Name)
		}
	}
	base := "/projects/" + projectID + "/tasks/" + taskID
	cases := []struct {
		name, method, path string
		args               map[string]any
	}{
		{"list_projects", "GET", "/projects/", map[string]any{"limit": 10, "cursor": "abc+/="}},
		{"list_my_tasks", "GET", "/my-work/", map[string]any{}},
		{"list_project_statuses", "GET", "/projects/" + projectID + "/statuses/", map[string]any{"projectId": projectID}},
		{"list_tasks", "GET", "/projects/" + projectID + "/tasks/", map[string]any{"projectId": projectID, "search": "login & logout"}},
		{"get_task", "GET", base + "/", map[string]any{"projectId": projectID, "taskId": taskID}},
		{"list_task_comments", "GET", base + "/comments/", map[string]any{"projectId": projectID, "taskId": taskID}},
		{"list_task_dependencies", "GET", base + "/dependencies/", map[string]any{"projectId": projectID, "taskId": taskID}},
		{"list_task_attachments", "GET", base + "/attachments/", map[string]any{"projectId": projectID, "taskId": taskID}},
		{"claim_task", "POST", base + "/claim", map[string]any{"projectId": projectID, "taskId": taskID, "statusId": statusID}},
		{"update_task_status", "PATCH", base + "/", map[string]any{"projectId": projectID, "taskId": taskID, "statusId": statusID}},
		{"add_comment", "POST", base + "/comments/", map[string]any{"projectId": projectID, "taskId": taskID, "body": "Working on login"}},
		{"submit_task_result", "POST", base + "/comments/", map[string]any{"projectId": projectID, "taskId": taskID, "summary": "Fixed login", "tests": "go test ./... passed", "pullRequestUrl": "https://github.com/example/repo/pull/1"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tt.name, Arguments: tt.args})
			if err != nil || res.IsError {
				t.Fatalf("tool failed: %+v, %v", res, err)
			}
			var got request
			select {
			case got = <-requests:
			default:
				t.Fatal("no API request")
			}
			if got.method != tt.method || got.path != tt.path {
				t.Fatalf("got %s %s, want %s %s", got.method, got.path, tt.method, tt.path)
			}
			if tt.name == "list_projects" && got.query != "cursor=abc%2B%2F%3D&limit=10" {
				t.Fatalf("incorrect pagination: %s", got.query)
			}
			if tt.name == "claim_task" && (len(got.body) != 1 || got.body["statusId"] != statusID) {
				t.Fatalf("claim must not accept caller identity: %v", got.body)
			}
			if tt.name == "submit_task_result" && !strings.Contains(got.body["body"].(string), "go test ./... passed") {
				t.Fatal("missing validation evidence")
			}
		})
	}
	select {
	case <-requests:
		t.Fatal("result submission must not implicitly update status")
	default:
	}
}

func TestMCPRejectsInvalidArgumentsBeforeAPI(t *testing.T) {
	cs := connectTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid arguments reached API")
		w.WriteHeader(500)
	})
	for _, tt := range []struct {
		name string
		args map[string]any
	}{
		{"get_task", map[string]any{"projectId": "../members", "taskId": taskID}},
		{"claim_task", map[string]any{"projectId": projectID, "taskId": taskID, "statusId": "bad"}},
		{"update_task_status", map[string]any{"projectId": projectID, "taskId": taskID, "statusId": ""}},
		{"list_my_tasks", map[string]any{"limit": 101}},
		{"add_comment", map[string]any{"projectId": projectID, "taskId": taskID, "body": "  "}},
		{"submit_task_result", map[string]any{"projectId": projectID, "taskId": taskID, "summary": "Fixed", "tests": ""}},
		{"submit_task_result", map[string]any{"projectId": projectID, "taskId": taskID, "summary": "Fixed", "tests": "Passed", "pullRequestUrl": "javascript:alert(1)"}},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tt.name, Arguments: tt.args})
		if err == nil && !res.IsError {
			t.Fatalf("invalid %s arguments succeeded", tt.name)
		}
	}
}

func TestMCPAPIErrorsAreToolErrors(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 409, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			cs := connectTest(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret-token internal details", code) })
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "claim_task", Arguments: map[string]any{"projectId": projectID, "taskId": taskID}})
			if err != nil || !res.IsError {
				t.Fatalf("expected tool error: %+v %v", res, err)
			}
			data, _ := json.Marshal(res)
			if strings.Contains(string(data), "secret-token") {
				t.Fatal("leaked API error body")
			}
		})
	}
}

func TestClientConfigurationAndRedirects(t *testing.T) {
	for _, endpoint := range []string{"", "http://example.com", "https://user:pass@example.com", "https://example.com?token=secret", "file:///tmp/api"} {
		if _, err := NewClient(Config{APIURL: endpoint, APIKey: "key", UserID: "user", AppID: "app"}); err == nil {
			t.Fatalf("accepted unsafe URL %q", endpoint)
		}
	}
	if _, err := NewClient(Config{APIURL: "https://example.com"}); err == nil {
		t.Fatal("accepted missing credentials")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect with credentials") }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	c, err := NewClient(Config{APIURL: source.URL, APIKey: "key", UserID: "user", AppID: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.call(context.Background(), "GET", "/", nil, nil); err == nil {
		t.Fatal("redirect succeeded")
	}
}
