package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}
}

func testDispatcher(t *testing.T, transport roundTripFunc, capacity int) *Dispatcher {
	t.Helper()
	d := newDispatcher(NewClient("https://notifications.test", &http.Client{Transport: transport}), 1, capacity, 0)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.Close(ctx); err != nil {
			t.Errorf("close dispatcher: %v", err)
		}
	})
	return d
}

func awaitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification worker")
	}
}

func TestDispatcherReturnsBeforeDeliveryAndSnapshotsPayload(t *testing.T) {
	started, release, delivered := make(chan struct{}), make(chan struct{}, 1), make(chan struct{})
	var received SendInput
	d := testDispatcher(t, func(r *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			return nil, err
		}
		close(delivered)
		return response(200), nil
	}, 1)
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := "original"
	input := SendInput{Body: &body, Data: map[string]any{"value": "original"}}
	returned := make(chan struct{})
	var sendErr error
	go func() {
		sendErr = d.Send(ctx, input)
		close(returned)
	}()
	awaitSignal(t, returned)
	if sendErr != nil {
		t.Fatal(sendErr)
	}
	awaitSignal(t, started)
	cancel()
	body = "changed"
	input.Data["value"] = "changed"
	// Release without closing here so the deferred close remains safe.
	release <- struct{}{}
	awaitSignal(t, delivered)
	if received.Body == nil || *received.Body != "original" || received.Data["value"] != "original" {
		t.Fatalf("queued payload was changed by the caller: %+v", received)
	}
}

func TestDispatcherRetriesOnlyTransientFailures(t *testing.T) {
	for _, tt := range []struct {
		name       string
		statuses   []int
		networkErr error
		want       int
	}{
		{"temporary HTTP failures", []int{503, 429, 200}, nil, 3},
		{"permanent HTTP failure", []int{400}, nil, 1},
		{"retry limit", []int{503, 503, 503}, nil, 3},
		{"network timeout", []int{200}, &net.DNSError{Err: "timeout", IsTimeout: true}, 2},
		{"connection closed", []int{200}, io.EOF, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			var deliveryID string
			d := testDispatcher(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if key := r.Header.Get("Idempotency-Key"); key == "" || (deliveryID != "" && deliveryID != key) {
					return nil, errors.New("delivery ID changed between retries")
				} else {
					deliveryID = key
				}
				if calls == 1 && tt.networkErr != nil {
					return nil, tt.networkErr
				}
				index := calls - 1
				if tt.networkErr != nil {
					index--
				}
				if index >= len(tt.statuses) {
					return nil, errors.New("unexpected additional delivery attempt")
				}
				return response(tt.statuses[index]), nil
			}, 1)
			if err := d.Send(context.Background(), SendInput{}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := d.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if calls != tt.want {
				t.Fatalf("delivery attempts = %d, want %d", calls, tt.want)
			}
		})
	}
}

func TestDispatcherQueueFullAndClosed(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{}, 1)
	calls := 0
	d := testDispatcher(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return response(200), nil
	}, 1)
	defer close(release)
	if err := d.Send(context.Background(), SendInput{}); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, started)
	if err := d.Send(context.Background(), SendInput{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Send(context.Background(), SendInput{}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("full queue error = %v", err)
	}
	release <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("shutdown delivered %d notifications, want 2", calls)
	}
	if err := d.Send(context.Background(), SendInput{}); !errors.Is(err, ErrDispatcherClosed) {
		t.Fatalf("closed dispatcher error = %v", err)
	}
}

func TestDispatcherShutdownCancelsBlockedDelivery(t *testing.T) {
	started := make(chan struct{})
	d := testDispatcher(t, func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}, 1)
	if err := d.Send(context.Background(), SendInput{}); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, started)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown error = %v", err)
	}
	awaitSignal(t, d.done)
}

func TestDispatcherRejectsCanceledAndInvalidJobs(t *testing.T) {
	calls := 0
	d := testDispatcher(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return response(200), nil
	}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Send(ctx, SendInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled enqueue error = %v", err)
	}
	if err := d.Send(context.Background(), SendInput{Data: map[string]any{"invalid": func() {}}}); err == nil {
		t.Fatal("unserializable notification was accepted")
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("invalid jobs resulted in %d delivery attempts", calls)
	}
}
