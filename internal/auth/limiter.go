package auth

import (
	"sync"
	"time"
)

// Limiter is a fixed-window rate limiter keyed by string (the client IP for
// login attempts). It is in-memory on purpose: the threat is one host guessing
// the admin password, not a distributed attack, and a restart resetting the
// window is acceptable for a single-admin app.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	clock  func() time.Time
	hits   map[string]*windowState
}

type windowState struct {
	count   int
	resetAt time.Time
}

// NewLimiter allows max attempts per window per key.
func NewLimiter(max int, window time.Duration, clock func() time.Time) *Limiter {
	if clock == nil {
		clock = time.Now
	}
	return &Limiter{max: max, window: window, clock: clock, hits: map[string]*windowState{}}
}

// Allow records an attempt and reports whether it is within the budget.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock()
	// Opportunistic cleanup keeps the map bounded without a background goroutine.
	if len(l.hits) > 1024 {
		for k, w := range l.hits {
			if now.After(w.resetAt) {
				delete(l.hits, k)
			}
		}
	}

	w, ok := l.hits[key]
	if !ok || now.After(w.resetAt) {
		l.hits[key] = &windowState{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	w.count++
	return w.count <= l.max
}
