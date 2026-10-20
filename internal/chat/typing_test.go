package chat

import (
	"testing"
	"time"
)

func TestTypingLimiterThrottlesStarts(t *testing.T) {
	limiter := newTypingLimiter()

	now := time.Unix(1000, 0)
	limiter.now = func() time.Time { return now }

	const (
		tenant = "tenant-a"
		user   = "alice"
		conv   = "conversation-a"
	)

	if !limiter.Allow(tenant, user, conv, true) {
		t.Fatal("first typing start was throttled")
	}

	if limiter.Allow(tenant, user, conv, true) {
		t.Fatal("duplicate typing start was not throttled")
	}

	// Another conversation has independent throttle state.
	if !limiter.Allow(tenant, user, "conversation-b", true) {
		t.Fatal("typing start in another conversation was throttled")
	}

	now = now.Add(typingThrottleInterval)

	if !limiter.Allow(tenant, user, conv, true) {
		t.Fatal("typing start after the interval was throttled")
	}

	// A stop frame always passes and clears the throttle...
	if !limiter.Allow(tenant, user, conv, false) {
		t.Fatal("typing stop was throttled")
	}

	// ...so an immediate restart is delivered.
	if !limiter.Allow(tenant, user, conv, true) {
		t.Fatal("typing restart after stop was throttled")
	}

	// A stop without an active start is pointless fan-out and is dropped.
	if limiter.Allow(tenant, user, "conversation-c", false) {
		t.Fatal("unpaired typing stop was published")
	}
}
