package auth

import (
	"context"
	"github.com/mhdph/taskmanager/internal/hosted"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHostedSupportMenuContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-APIKEY") != "token" || r.Header.Get("USER-ID") != "alice" || r.Header.Get("APP-ID") != "tenant" {
			t.Error("authentication headers missing")
		}
		_, _ = w.Write([]byte(`{"data":{"nupProfileAppInfo":{"user":{"id":"alice"},"app":{"id":"tenant"},"acl":{"name":"Member"}}}}`))
	}))
	defer server.Close()
	resolver := NewResolver(hosted.NewClient(server.URL, server.Client()), NewIdentityCache(time.Minute))
	for _, value := range []string{"yes", "no", "true", "false", ""} {
		identity, err := resolver.Resolve(context.Background(), ResolveInput{Token: "token", UserID: "alice", AppID: "tenant", FromSupportMenu: value})
		if err != nil || identity.IsSupport != (value == "yes" || value == "true") {
			t.Fatalf("support value %q: %+v %v", value, identity, err)
		}
	}
}
