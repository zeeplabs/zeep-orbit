package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
	limit   int
	window  time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		windows: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	prev := rl.windows[key]
	var fresh []time.Time
	for _, t := range prev {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}

	if len(fresh) == 0 {
		delete(rl.windows, key)
	} else {
		rl.windows[key] = fresh
	}

	if len(fresh) >= rl.limit {
		return false
	}

	rl.windows[key] = append(fresh, now)
	return true
}

// remoteHost strips the port from r.RemoteAddr so the limiter buckets by
// client host, not by client host+ephemeral-port (which would give every
// new TCP connection its own bucket, making the limiter a no-op).
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(remoteHost(r)) {
			writeError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}
