package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadinessDependencyFailures(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		databaseErr, storageErr error
		want                    int
	}{
		{"healthy", nil, nil, http.StatusOK},
		{"database unavailable", errors.New("private database details"), nil, http.StatusServiceUnavailable},
		{"storage unavailable", nil, errors.New("private storage details"), http.StatusServiceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			checks := 0
			check := func(err error) func(context.Context) error {
				return func(ctx context.Context) error {
					checks++
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 2*time.Second {
						t.Error("readiness check lacks a bounded deadline")
					}
					return err
				}
			}
			w := httptest.NewRecorder()
			readinessHandler(check(tt.databaseErr), check(tt.storageErr))(w, httptest.NewRequest(http.MethodGet, "/health", nil))
			if w.Code != tt.want || checks != 2 {
				t.Fatalf("readiness status=%d checks=%d", w.Code, checks)
			}
			if strings.Contains(w.Body.String(), "private") {
				t.Fatal("health endpoint exposed dependency error details")
			}
		})
	}
}

func TestReadinessHonorsRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	check := func(ctx context.Context) error { return ctx.Err() }
	w := httptest.NewRecorder()
	readinessHandler(check, check)(w, httptest.NewRequest(http.MethodGet, "/health", nil).WithContext(ctx))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("canceled health request reported ready")
	}
}
