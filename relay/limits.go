package relay

import (
	"sync"
	"time"
)

// Limits are the relay's infrastructure caps. They are returned to clients
// when a session is provisioned so agents read them instead of hardcoding.
type Limits struct {
	MessagesPerMinute    int   `json:"messages_per_minute"`
	MaxBodyBytes         int64 `json:"max_body_bytes"`
	MaxFileBytes         int64 `json:"max_file_bytes"`
	MaxStorageBytes      int64 `json:"max_storage_bytes"`
	MaxMessages          int64 `json:"max_messages"`
	InactivityTTLSeconds int64 `json:"inactivity_ttl_seconds"`
	GraceSeconds         int64 `json:"grace_seconds"`
	MaxWaitSeconds       int   `json:"max_wait_seconds"`
	SessionsPerIPPerHour int   `json:"sessions_per_ip_per_hour"` // 0 disables
}

// DefaultLimits are the contract defaults.
var DefaultLimits = Limits{
	MessagesPerMinute:    60,
	MaxBodyBytes:         64 << 10,
	MaxFileBytes:         100 << 20,
	MaxStorageBytes:      1 << 30,
	MaxMessages:          10000,
	InactivityTTLSeconds: 24 * 3600,
	GraceSeconds:         24 * 3600,
	MaxWaitSeconds:       55,
	SessionsPerIPPerHour: 30,
}

// limiter counts events per key in fixed windows.
type limiter struct {
	window time.Duration
	max    int
	mu     sync.Mutex
	m      map[string]*bucket
}

type bucket struct {
	start time.Time
	n     int
}

func newLimiter(window time.Duration, max int) *limiter {
	return &limiter{window: window, max: max, m: make(map[string]*bucket)}
}

// allow records one event for key. If the window is full it returns false
// and the time until the window resets.
func (l *limiter) allow(key string, now time.Time) (bool, time.Duration) {
	if l.max <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.m[key]
	if b == nil || now.Sub(b.start) >= l.window {
		b = &bucket{start: now}
		l.m[key] = b
	}
	if b.n >= l.max {
		return false, b.start.Add(l.window).Sub(now)
	}
	b.n++
	return true, 0
}

// prune drops expired windows.
func (l *limiter) prune(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.m {
		if now.Sub(b.start) >= l.window {
			delete(l.m, k)
		}
	}
}
