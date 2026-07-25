// Package tokencache holds the in-memory app-token activity cache shared
// between the request-path middleware (server) and the dashboard token
// management endpoints (dashboard), which need to invalidate entries on
// revoke/regenerate without an import cycle between those two packages.
package tokencache

import (
	"sync"
	"time"
)

const ttl = 30 * time.Second

type entry struct {
	active bool
	at     time.Time
}

var (
	mu    sync.RWMutex
	cache = make(map[string]entry)
)

// IsActive returns the cached active state for jti and whether the entry is still fresh.
func IsActive(jti string) (active bool, fresh bool) {
	mu.RLock()
	e, ok := cache[jti]
	mu.RUnlock()
	if !ok || time.Since(e.at) > ttl {
		return false, false
	}
	return e.active, true
}

// Set stores the active state for jti.
func Set(jti string, active bool) {
	mu.Lock()
	cache[jti] = entry{active: active, at: time.Now()}
	mu.Unlock()
}

// Invalidate drops any cached entry for jti, forcing the next check to hit the DB.
func Invalidate(jti string) {
	mu.Lock()
	delete(cache, jti)
	mu.Unlock()
}
