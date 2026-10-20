package chat

import (
	"sync"
	"time"
)

const (
	// typingThrottleInterval bounds how often one user can publish a
	// "started typing" event for the same conversation. Without it a
	// keystroke-per-event client would turn every key into a database
	// membership read and a NOTIFY.
	typingThrottleInterval = 2 * time.Second

	// typingEntryTTL lets idle throttle entries be reclaimed.
	typingEntryTTL = 2 * time.Minute

	typingPruneThreshold = 1024
)

// typingLimiter is per-instance throttling state. It does not need to be
// shared: each instance throttles the frames it receives.
type typingLimiter struct {
	mu sync.Mutex

	last map[string]time.Time

	now func() time.Time
}

func newTypingLimiter() *typingLimiter {
	return &typingLimiter{
		last: make(map[string]time.Time),

		now: time.Now,
	}
}

// Allow reports whether a typing frame may be published. Stop frames are
// only forwarded when a start is active, which prevents clients from
// spamming stop frames. Stopping clears the throttle so a quick
// start/stop/start sequence is not swallowed.
func (l *typingLimiter) Allow(
	tenantID string,
	userID string,
	conversationID string,
	typing bool,
) bool {
	key := tenantID + "\x00" + userID + "\x00" + conversationID

	l.mu.Lock()
	defer l.mu.Unlock()

	if !typing {
		if _, ok := l.last[key]; !ok {
			return false
		}

		delete(l.last, key)

		return true
	}

	now := l.now()

	if last, ok := l.last[key]; ok &&
		now.Sub(last) < typingThrottleInterval {

		return false
	}

	l.last[key] = now

	if len(l.last) > typingPruneThreshold {
		l.prune(now)
	}

	return true
}

func (l *typingLimiter) prune(now time.Time) {
	for key, last := range l.last {
		if now.Sub(last) > typingEntryTTL {
			delete(l.last, key)
		}
	}
}
