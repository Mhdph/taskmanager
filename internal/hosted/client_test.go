package hosted

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHostedCredentialsNeverFollowRedirect(t *testing.T) {
	var leaked atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(true)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client := &http.Client{}
	_, err := NewClient(origin.URL, client).NupProfileAppInfo(context.Background(), ProfileRequest{Token: "secret"})
	if err == nil || leaked.Load() {
		t.Fatal("hosted redirect followed or accepted")
	}
	if client.CheckRedirect != nil {
		t.Fatal("shared HTTP client was mutated")
	}
}

func TestHostedResponseSizeIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"` + strings.Repeat("x", 2<<20) + `"}]}`))
	}))
	defer server.Close()
	_, err := NewClient(server.URL, server.Client()).NupProfileAppInfo(context.Background(), ProfileRequest{})
	if err == nil || !strings.Contains(err.Error(), "decode graphql response") {
		t.Fatalf("oversized hosted response was not rejected: %v", err)
	}
}
