package server

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

type perAppLimiter struct {
	mu       sync.Mutex
	entries  map[string]*rlEntry
	max      int
	window   time.Duration
}

type rlEntry struct {
	count       int
	windowStart time.Time
}

type AppRateLimiter struct {
	mu      sync.RWMutex
	limiters map[string]*perAppLimiter
}

func NewAppRateLimiter() *AppRateLimiter {
	return &AppRateLimiter{
		limiters: make(map[string]*perAppLimiter),
	}
}

func (arl *AppRateLimiter) getOrCreate(appName string, rpm int) *perAppLimiter {
	arl.mu.RLock()
	l, ok := arl.limiters[appName]
	arl.mu.RUnlock()
	if ok {
		return l
	}

	arl.mu.Lock()
	defer arl.mu.Unlock()
	if l, ok := arl.limiters[appName]; ok {
		return l
	}
	if rpm <= 0 {
		rpm = 60
	}
	l = &perAppLimiter{
		entries: make(map[string]*rlEntry),
		max:     rpm,
		window:  time.Minute,
	}
	arl.limiters[appName] = l
	return l
}

func (l *perAppLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[ip]
	if !ok || now.Sub(e.windowStart) > l.window {
		l.entries[ip] = &rlEntry{count: 1, windowStart: now}
		return true
	}
	e.count++
	return e.count <= l.max
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func RateLimitMiddleware(arl *AppRateLimiter, reg *registry.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			appName := chi.URLParam(r, "app")
			if appName == "" {
				next.ServeHTTP(w, r)
				return
			}
			app, ok := reg.Get(appName)
			if !ok || app.RateLimit == nil || !app.RateLimit.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			limiter := arl.getOrCreate(appName, app.RateLimit.RequestsPerMinute)
			if !limiter.allow(remoteIP(r)) {
				w.Header().Set("Retry-After", "60")
				writeError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
