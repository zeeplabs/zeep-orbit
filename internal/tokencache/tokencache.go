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
	// gen moves on every Invalidate and Clear. A fill that read the database
	// before the move may hold a state the revoke already replaced, so
	// SetIfGeneration drops it.
	gen uint64
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

// Generation returns the current cache generation. Read it before the
// database lookup and pass it to SetIfGeneration.
func Generation() uint64 {
	mu.RLock()
	defer mu.RUnlock()
	return gen
}

// SetIfGeneration stores the active state for jti only if no Invalidate or
// Clear ran since g was read. Otherwise a lookup that read "active" just
// before a revoke would cache the revoked token as active for the full TTL.
func SetIfGeneration(jti string, active bool, g uint64) {
	mu.Lock()
	if gen == g {
		cache[jti] = entry{active: active, at: time.Now()}
	}
	mu.Unlock()
}

// Invalidate drops any cached entry for jti, forcing the next check to hit the DB.
func Invalidate(jti string) {
	mu.Lock()
	delete(cache, jti)
	gen++
	mu.Unlock()
}

// Clear drops every cached entry. Invalidate only reaches the pod that
// handled the revoke; the registry syncer calls Clear on every pod when the
// shared state version moves (a revocation bumps it), so a revoked token
// stops working everywhere within one poll instead of up to the 30s TTL.
func Clear() {
	mu.Lock()
	cache = make(map[string]entry)
	gen++
	mu.Unlock()
}
