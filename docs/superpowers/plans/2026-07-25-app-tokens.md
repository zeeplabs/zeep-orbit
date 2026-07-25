# App Tokens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add dashboard-managed JWTs for apps without email auth — create, revoke, refresh, and regenerate secret.

**Architecture:** New `zeep_system.app_tokens` DB table. New store layer `internal/dashboard/app_tokens_store.go`. Dashboard handler extends `internal/dashboard/handler.go`. JWT middleware adds jti lookup with in-memory cache. New auth handler method for refresh. New UI tab in `AppDetailsPage.tsx`.

**Tech Stack:** Go (chi, pgx, golang-jwt), React (TanStack Query, shadcn/ui), PostgreSQL

## Global Constraints

- Feature is exclusive to apps with `auth_email_enabled = false`
- All CRUD endpoints on apps without email auth already require JWTMiddleware
- Email auth JWTs are unaffected (no `token_type` claim)
- JWT secret is auto-generated via `gen_random_bytes(32)` on app creation
- `app.JWTSecret` is cleared from create/update responses but returned in GetApp

---

### Task 1: Database migration — `zeep_system.app_tokens` table

**Files:**
- Create: `internal/dashboard/migrations/005_app_tokens.up.sql`
- Modify: `internal/dashboard/provisioner.go` (run migration on startup)

**Interfaces:**
- Consumes: existing `zeep_system.apps` table (FK)
- Produces: `zeep_system.app_tokens` table

- [ ] **Step 1: Create migration file**

```sql
CREATE TABLE IF NOT EXISTS zeep_system.app_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id       UUID NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    jti          TEXT NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_app_tokens_app_id ON zeep_system.app_tokens(app_id);
CREATE INDEX IF NOT EXISTS idx_app_tokens_jti     ON zeep_system.app_tokens(jti);
```

- [ ] **Step 2: Check how existing migrations run**

Look at `internal/dashboard/provisioner.go` to find where DB schema is initialized. Run the migration there.

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/migrations/005_app_tokens.up.sql
git commit -m "feat: add app_tokens table for JWT management"
```

---

### Task 2: Store layer — `app_tokens_store.go`

**Files:**
- Create: `internal/dashboard/app_tokens_store.go`
- Modify: none

**Interfaces:**
- Consumes: `*db.Pool`, `context.Context`, app ID (UUID)
- Produces: `AppTokenRow` struct, CRUD functions

- [ ] **Step 1: Write the store file

```go
package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type AppTokenRow struct {
	ID         string     `json:"id"`
	AppID      string     `json:"app_id"`
	Name       string     `json:"name"`
	JTI        string     `json:"jti"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreateAppTokenInput struct {
	AppID     string
	Name      string
	ExpiresAt *time.Time
}

func ListAppTokens(ctx context.Context, pool *db.Pool, appID string) ([]AppTokenRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, app_id, name, jti, expires_at, revoked_at, last_used_at, created_at
		 FROM zeep_system.app_tokens WHERE app_id = $1 ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []AppTokenRow
	for rows.Next() {
		var t AppTokenRow
		if err := rows.Scan(&t.ID, &t.AppID, &t.Name, &t.JTI, &t.ExpiresAt, &t.RevokedAt, &t.LastUsedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func CreateAppToken(ctx context.Context, pool *db.Pool, input CreateAppTokenInput) (*AppTokenRow, error) {
	jti := uuid.New().String()
	var t AppTokenRow
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.app_tokens (app_id, name, jti, expires_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, app_id, name, jti, expires_at, revoked_at, last_used_at, created_at`,
		input.AppID, input.Name, jti, input.ExpiresAt,
	).Scan(&t.ID, &t.AppID, &t.Name, &t.JTI, &t.ExpiresAt, &t.RevokedAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func RevokeAppToken(ctx context.Context, pool *db.Pool, tokenID string, appID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.app_tokens SET revoked_at = now() WHERE id = $1 AND app_id = $2 AND revoked_at IS NULL`,
		tokenID, appID)
	if err != nil {
		return err
	}
	return nil
}

func RevokeAllAppTokens(ctx context.Context, pool *db.Pool, appID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.app_tokens SET revoked_at = now() WHERE app_id = $1 AND revoked_at IS NULL`,
		appID)
	return err
}

func GetAppTokenByJTI(ctx context.Context, pool *db.Pool, jti string) (*AppTokenRow, error) {
	var t AppTokenRow
	err := pool.QueryRow(ctx,
		`SELECT id, app_id, name, jti, expires_at, revoked_at, last_used_at, created_at
		 FROM zeep_system.app_tokens WHERE jti = $1`, jti,
	).Scan(&t.ID, &t.AppID, &t.Name, &t.JTI, &t.ExpiresAt, &t.RevokedAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func TouchAppToken(ctx context.Context, pool *db.Pool, jti string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.app_tokens SET last_used_at = now() WHERE jti = $1`, jti)
	return err
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/app_tokens_store.go
git commit -m "feat: add app_tokens store layer"
```

---

### Task 3: JWT — new claims and issuer for app tokens

**Files:**
- Modify: `internal/auth/jwt.go`

**Interfaces:**
- Consumes: secret, jti, appName, expiration (optional)
- Produces: `AppTokenClaims` struct, `IssueAppTokenJWT()`, `ParseAppTokenJWT()`

- [ ] **Step 1: Add app token claims and issuer to jwt.go

After the existing `Claims` struct and `IssueJWT`:

```go
type AppTokenClaims struct {
	TokenType string `json:"token_type"`
	jwtlib.RegisteredClaims
}

func IssueAppTokenJWT(secret []byte, jti, appName string, expiresAt *time.Time) (string, error) {
	now := time.Now()
	c := AppTokenClaims{
		TokenType: "app_token",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   appName,
			ID:        jti,
			IssuedAt:  jwtlib.NewNumericDate(now),
		},
	}
	if expiresAt != nil {
		c.ExpiresAt = jwtlib.NewNumericDate(*expiresAt)
	}
	t := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	return t.SignedString(secret)
}

func ParseAppTokenJWT(secret []byte, raw string) (*AppTokenClaims, error) {
	t, err := jwtlib.ParseWithClaims(raw, &AppTokenClaims{}, func(t *jwtlib.Token) (any, error) {
		return secret, nil
	}, jwtlib.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid {
		return nil, jwtlib.ErrTokenSignatureInvalid
	}
	c, ok := t.Claims.(*AppTokenClaims)
	if !ok {
		return nil, jwtlib.ErrTokenInvalidClaims
	}
	return c, nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/auth/jwt.go
git commit -m "feat: add AppTokenClaims and IssueAppTokenJWT"
```

---

### Task 4: Dashboard handler — token endpoints

**Files:**
- Modify: `internal/dashboard/handler.go`

**Interfaces:**
- Consumes: store functions from Task 2, JWT functions from Task 3
- Produces: `ListAppTokens`, `CreateAppToken`, `RevokeAppToken`, `RegenerateSecret`, `GetAppSecret` handler methods

- [ ] **Step 1: Add token expiration options as a helper**

Add to `handler.go`:

```go
var tokenExpirationOptions = map[string]*time.Duration{
	"7d":   durationPtr(7 * 24 * time.Hour),
	"30d":  durationPtr(30 * 24 * time.Hour),
	"365d": durationPtr(365 * 24 * time.Hour),
	"never": nil,
}

func durationPtr(d time.Duration) *time.Duration { return &d }
```

- [ ] **Step 2: Add ListAppTokens handler**

```go
func (h *Handler) ListAppTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, err := GetApp(r.Context(), h.pool, appID, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if app.AuthEmailEnabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app tokens only available for apps without email auth"})
		return
	}

	tokens, err := ListAppTokens(r.Context(), h.pool, appID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to list tokens", err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}
```

- [ ] **Step 3: Add CreateAppToken handler**

```go
func (h *Handler) CreateAppToken(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, err := GetApp(r.Context(), h.pool, appID, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if app.AuthEmailEnabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app tokens only available for apps without email auth"})
		return
	}

	var body struct {
		Name       string `json:"name"`
		Expiration string `json:"expiration"` // "7d", "30d", "365d", "never"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	dur, ok := tokenExpirationOptions[body.Expiration]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expiration"})
		return
	}

	var expiresAt *time.Time
	if dur != nil {
		t := time.Now().Add(*dur)
		expiresAt = &t
	}

	tokenRow, err := CreateAppToken(r.Context(), h.pool, CreateAppTokenInput{
		AppID: appID, Name: body.Name, ExpiresAt: expiresAt,
	})
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to create token", err)
		return
	}

	registryApp, _ := h.reg.Get(app.Name)
	jwtStr, err := auth.IssueAppTokenJWT(
		[]byte(registryApp.Config.Auth.JWTSecret),
		tokenRow.JTI, app.Name, expiresAt,
	)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to issue jwt", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"token": jwtStr,
		"row":   tokenRow,
	})
	h.audit(r.Context(), user.ID, user.Email, "app.token.create", "app", app.ID, app.Name, nil, r.RemoteAddr)
}
```

- [ ] **Step 4: Add RevokeAppToken handler**

```go
func (h *Handler) RevokeAppToken(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	tokenID := chi.URLParam(r, "tokenId")

	if err := RevokeAppToken(r.Context(), h.pool, tokenID, appID); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to revoke token", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "token revoked"})
	h.audit(r.Context(), user.ID, user.Email, "app.token.revoke", "app", appID, "", nil, r.RemoteAddr)
}
```

- [ ] **Step 5: Add RegenerateSecret handler**

```go
func (h *Handler) RegenerateAppSecret(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")

	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirmation required"})
		return
	}

	// generate new secret
	var newSecret string
	err := h.pool.QueryRow(r.Context(),
		`UPDATE zeep_system.apps SET jwt_secret = encode(gen_random_bytes(32), 'hex') WHERE id = $1 RETURNING jwt_secret`,
		appID,
	).Scan(&newSecret)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to regenerate secret", err)
		return
	}

	// revoke all tokens
	if err := RevokeAllAppTokens(r.Context(), h.pool, appID); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to revoke tokens", err)
		return
	}

	// refresh registry
	registryApp, _ := h.reg.Get(app.Name)
	if registryApp != nil {
		registryApp.Config.Auth.JWTSecret = newSecret
		h.reg.Register(registryApp)
	}

	writeJSON(w, http.StatusOK, map[string]string{"jwt_secret": newSecret})
	h.audit(r.Context(), user.ID, user.Email, "app.secret.regenerate", "app", appID, "", nil, r.RemoteAddr)
}
```

- [ ] **Step 6: Add GetAppSecret handler**

```go
func (h *Handler) GetAppSecret(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, err := GetApp(r.Context(), h.pool, appID, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"jwt_secret": app.JWTSecret})
	h.audit(r.Context(), user.ID, user.Email, "app.secret.view", "app", appID, app.Name, nil, r.RemoteAddr)
}
```

- [ ] **Step 7: Add imports**

Ensure handler.go imports `"github.com/zeeplabs/zeep-orbit/internal/auth"`, `"encoding/json"`, `"time"`.

- [ ] **Step 8: Commit**

```bash
git add internal/dashboard/handler.go
git commit -m "feat: add dashboard token management endpoints"
```

---

### Task 5: Server routing — wire dashboard token endpoints

**Files:**
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: `dashH.ListAppTokens`, `dashH.CreateAppToken`, `dashH.RevokeAppToken`, `dashH.RegenerateAppSecret`, `dashH.GetAppSecret`

- [ ] **Step 1: Add routes in the dashboard section**

After the existing app routes (line 177):

```go
r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/tokens", dashH.ListAppTokens)
r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/tokens", dashH.CreateAppToken)
r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/tokens/{tokenId}/revoke", dashH.RevokeAppToken)
r.With(dashboard.RequireAuth(pool)).Post("/api/apps/{id}/regenerate-secret", dashH.RegenerateAppSecret)
r.With(dashboard.RequireAuth(pool)).Get("/api/apps/{id}/secret", dashH.GetAppSecret)
```

- [ ] **Step 2: Commit**

```bash
git add internal/server/server.go
git commit -m "feat: wire dashboard token management routes"
```

---

### Task 6: Middleware — jti validation cache

**Files:**
- Modify: `internal/server/middleware.go`

**Interfaces:**
- Consumes: `GetAppTokenByJTI` from store
- Produces: JTI validation middleware in `JWTMiddleware`

- [ ] **Step 1: Add jti cache map and mutex to middleware.go**

```go
var (
	jtiCache     = make(map[string]bool)
	jtiCacheMu   sync.RWMutex
	jtiCacheTTL  = 30 * time.Second
	jtiCacheLast = make(map[string]time.Time)
)
```

- [ ] **Step 2: Add cache helper functions**

```go
func isTokenActiveCached(jti string) (bool, bool) {
	jtiCacheMu.RLock()
	active, ok := jtiCache[jti]
	last := jtiCacheLast[jti]
	jtiCacheMu.RUnlock()
	if !ok || time.Since(last) > jtiCacheTTL {
		return false, false
	}
	return active, true
}

func setTokenCache(jti string, active bool) {
	jtiCacheMu.Lock()
	jtiCache[jti] = active
	jtiCacheLast[jti] = time.Now()
	jtiCacheMu.Unlock()
}
```

- [ ] **Step 3: Modify JWTMiddleware to check jti**

After the JWT signature validation succeeds (after line 97), add:

```go
// check for app_token jti validation via MapClaims
var mapClaims jwtlib.MapClaims
if _, err := jwtlib.ParseWithClaims(rawToken, &mapClaims, nil, jwtlib.WithValidMethods([]string{"HS256"})); err == nil {
	if tokenType, _ := mapClaims["token_type"].(string); tokenType == "app_token" {
		jti, _ := mapClaims["jti"].(string)
		if jti == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		active, cached := isTokenActiveCached(jti)
		if !cached {
			tokenRow, err := GetAppTokenByJTI(r.Context(), pool, jti)
			if err != nil || tokenRow == nil {
				setTokenCache(jti, false)
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			active = tokenRow.RevokedAt == nil
			if active && tokenRow.ExpiresAt != nil && time.Now().After(*tokenRow.ExpiresAt) {
				active = false
			}
			setTokenCache(jti, active)
		}

		if !active {
			writeError(w, http.StatusUnauthorized, "token revoked or expired")
			return
		}

		// fire-and-forget update last_used_at
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			TouchAppToken(ctx, pool, jti)
		}()
	}
}
```

Actually, looking at the current code, `JWTMiddleware` only receives `reg *registry.Registry`. We need to either:
a) Pass the pool to JWTMiddleware
b) Access the pool from a global
c) Use the pool from the handler

Looking at the server.go, the pool is available. Let me change `JWTMiddleware` to accept a pool parameter too.

- [ ] **Step 4: Update JWTMiddleware signature**

Change:
```go
func JWTMiddleware(reg *registry.Registry) func(http.Handler) http.Handler {
```
To:
```go
func JWTMiddleware(reg *registry.Registry, pool *db.Pool) func(http.Handler) http.Handler {
```

Update all calls in `server.go` from `JWTMiddleware(reg)` to `JWTMiddleware(reg, pool)`.

- [ ] **Step 5: Update middleware body with pool**

Replace `GetAppTokenByJTI(r.Context(), nil, claims.ID)` with `GetAppTokenByJTI(r.Context(), pool, claims.ID)` and `TouchAppToken(ctx, nil, claims.ID)` with `TouchAppToken(ctx, pool, claims.ID)`.

- [ ] **Step 6: Add imports to middleware.go**

Add `"sync"`, `"time"`, `"context"`, and `"github.com/zeeplabs/zeep-orbit/internal/db"` to imports.

- [ ] **Step 7: Update test helper in middleware_test.go**

Change `buildRouter` to accept the new signature:

```go
func buildRouter(reg *registry.Registry) http.Handler {
	r := chi.NewRouter()
	r.With(JWTMiddleware(reg, nil)).Get("/{app}/{table}", func(w http.ResponseWriter, r *http.Request) {
		app, ok := AppFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusInternalServerError, "app not in context")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"app": app.Config.Name})
	})
	return r
}
```

- [ ] **Step 8: Commit**

```bash
git add internal/server/middleware.go internal/server/server.go
git commit -m "feat: add jti validation to JWTMiddleware with cache"
```

---

### Task 7: Auth handler — token refresh endpoint

**Files:**
- Modify: `internal/auth/handler.go`

**Interfaces:**
- Consumes: `auth.IssueAppTokenJWT`, `GetAppTokenByJTI`, `TouchAppToken` from store
- Produces: `POST /{app}/auth/token/refresh` (for app tokens)

- [ ] **Step 1: Add TokenRefresh handler to auth/handler.go**

```go
// TokenRefresh handles POST /{app}/auth/token/refresh for app tokens
func (h *Handler) TokenRefresh(w http.ResponseWriter, r *http.Request) {
	app, ok := h.appWithoutEmail(w, r)
	if !ok {
		return
	}

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rawToken := strings.TrimPrefix(authHeader, "Bearer ")

	secret := []byte(app.Config.Auth.JWTSecret)
	claims, err := ParseAppTokenJWT(secret, rawToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if claims.TokenType != "app_token" {
		writeError(w, http.StatusUnauthorized, "invalid token type")
		return
	}

	tokenRow, err := GetAppTokenByJTI(r.Context(), h.pool, claims.ID)
	if err != nil || tokenRow == nil {
		writeError(w, http.StatusUnauthorized, "token not found")
		return
	}
	if tokenRow.RevokedAt != nil {
		writeError(w, http.StatusUnauthorized, "token revoked")
		return
	}
	if tokenRow.ExpiresAt != nil && time.Now().After(*tokenRow.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, "token expired")
		return
	}

	newJWT, err := IssueAppTokenJWT(secret, claims.ID, app.Config.Name, tokenRow.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": newJWT})
}
```

- [ ] **Step 2: Add appWithoutEmail helper**

```go
func (h *Handler) appWithoutEmail(w http.ResponseWriter, r *http.Request) (*registry.App, bool) {
	appName := chi.URLParam(r, "app")
	app, ok := h.reg.Get(appName)
	if !ok || app.Config.Auth.Providers.Email {
		writeError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	return app, true
}
```

- [ ] **Step 3: Add imports**

Add `"strings"` to auth/handler.go imports if not present.

- [ ] **Step 4: Wire the route in server.go**

```go
// inside the /{app}/auth route group
r.Post("/token/refresh", ah.TokenRefresh)
```

- [ ] **Step 5: Commit**

```bash
git add internal/auth/handler.go internal/server/server.go
git commit -m "feat: add app token refresh endpoint"
```

---

### Task 8: Dashboard UI — Tokens tab

**Files:**
- Modify: `internal/dashboard/ui/src/pages/AppDetailsPage.tsx`
- Modify: `internal/dashboard/ui/src/lib/api.ts`

- [ ] **Step 1: Add API functions to api.ts**

```typescript
export interface AppToken {
  id: string
  app_id: string
  name: string
  jti: string
  expires_at: string | null
  revoked_at: string | null
  last_used_at: string | null
  created_at: string
}

export interface CreateAppTokenInput {
  name: string
  expiration: '7d' | '30d' | '365d' | 'never'
}

export interface CreateAppTokenResponse {
  token: string
  row: AppToken
}

export function useAppTokens(appId: string): UseQueryResult<AppToken[]> {
  return useQuery({
    queryKey: ['app-tokens', appId],
    queryFn: () => apiFetch<AppToken[]>(`/dashboard/api/apps/${appId}/tokens`),
    enabled: Boolean(appId),
  })
}

export function useCreateAppToken(appId: string): UseMutationResult<CreateAppTokenResponse, Error, CreateAppTokenInput> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input) =>
      apiFetch<CreateAppTokenResponse>(`/dashboard/api/apps/${appId}/tokens`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app-tokens', appId] })
    },
  })
}

export function useRevokeAppToken(appId: string): UseMutationResult<{ message: string }, Error, string> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (tokenId) =>
      apiFetch(`/dashboard/api/apps/${appId}/tokens/${tokenId}/revoke`, { method: 'POST' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['app-tokens', appId] })
    },
  })
}

export function useRegenerateAppSecret(appId: string): UseMutationResult<{ jwt_secret: string }, Error, void> {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      apiFetch<{ jwt_secret: string }>(`/dashboard/api/apps/${appId}/regenerate-secret`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['apps'] })
      qc.invalidateQueries({ queryKey: ['app-tokens', appId] })
    },
  })
}

export function useAppSecret(appId: string): UseQueryResult<{ jwt_secret: string }> {
  return useQuery({
    queryKey: ['app-secret', appId],
    queryFn: () => apiFetch<{ jwt_secret: string }>(`/dashboard/api/apps/${appId}/secret`),
    enabled: Boolean(appId),
  })
}
```

- [ ] **Step 2: Add TokensTab component to AppDetailsPage.tsx**

```tsx
import { Key, Plus, Copy, Eye, EyeOff, RefreshCw, X, AlertTriangle } from "lucide-react"
import {
  useAppTokens,
  useCreateAppToken,
  useRevokeAppToken,
  useRegenerateAppSecret,
  useAppSecret,
  AppToken,
} from "../lib/api"

// --- TokensTab component ---
function TokensTab({ app }: { app: NonNullable<ReturnType<typeof useApp>["data"]> }) {
  const { data: tokens, isLoading } = useAppTokens(app.id)
  const createToken = useCreateAppToken(app.id)
  const revokeToken = useRevokeAppToken(app.id)
  const regenerateSecret = useRegenerateAppSecret(app.id)
  const [showCreate, setShowCreate] = useState(false)
  const [showSecret, setShowSecret] = useState(false)
  const [showRegenerateConfirm, setShowRegenerateConfirm] = useState(false)
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null)

  if (app.auth_email_enabled) {
    return (
      <div className="rounded-2xl border border-yellow-500/[0.18] bg-yellow-500/[0.06] px-6 py-5 text-sm text-yellow-400">
        App tokens estão disponíveis apenas para apps sem autenticação por e-mail.
      </div>
    )
  }

  const statusBadge = (t: AppToken) => {
    if (t.revoked_at) return <span className="text-[11px] font-medium text-red-400 bg-red-500/[0.12] px-2 py-0.5 rounded-full">Revogado</span>
    if (t.expires_at && new Date(t.expires_at) < new Date()) return <span className="text-[11px] font-medium text-yellow-400 bg-yellow-500/[0.12] px-2 py-0.5 rounded-full">Expirado</span>
    return <span className="text-[11px] font-medium text-emerald-400 bg-emerald-500/[0.12] px-2 py-0.5 rounded-full">Ativo</span>
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="h-6 w-1 rounded-full" style={{ background: "linear-gradient(to bottom, var(--brand-primary), var(--brand-secondary))" }} />
          <p className="text-[15px] font-extrabold text-[#F8FAFC]">Access Tokens</p>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={() => setShowRegenerateConfirm(true)} className="flex items-center gap-1.5 px-3 py-1.5 rounded-full border border-white/[0.12] bg-white/[0.05] text-[#94A3B8] text-[13px] font-medium cursor-pointer hover:text-white transition-colors">
            <RefreshCw size={14} /> Regenerar Secret
          </button>
          <button onClick={() => setShowCreate(true)} className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-full border border-white/[0.12] bg-white/[0.05] text-[#F8FAFC] text-[13px] font-medium cursor-pointer hover:bg-white/[0.08] transition-colors">
            <Plus size={14} /> Novo Token
          </button>
        </div>
      </div>

      {/* JWT Secret */}
      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-4">
        <div className="flex items-center justify-between mb-2">
          <p className="text-[12px] font-semibold text-[#94A3B8] uppercase tracking-wider">JWT Secret</p>
          <button
            onClick={() => {
              if (!revealedSecret) {
                fetch(`/dashboard/api/apps/${app.id}/secret`, { credentials: 'include' })
                  .then(r => r.json())
                  .then(d => setRevealedSecret(d.jwt_secret))
              } else {
                setRevealedSecret(null)
              }
            }}
            className="flex items-center gap-1 text-[11px] text-[#94A3B8] hover:text-white bg-transparent border-none cursor-pointer"
          >
            {revealedSecret ? <EyeOff size={13} /> : <Eye size={13} />}
            {revealedSecret ? 'Ocultar' : 'Revelar'}
          </button>
        </div>
        {revealedSecret ? (
          <div className="flex items-center gap-2 bg-black/30 rounded-xl px-4 py-3">
            <code className="text-sm text-[#B3D1FF] break-all font-mono flex-1">{revealedSecret}</code>
            <button onClick={() => navigator.clipboard.writeText(revealedSecret)} className="shrink-0 p-1.5 rounded-lg hover:bg-white/[0.08] text-[#94A3B8] hover:text-[#F8FAFC] transition-colors">
              <Copy size={14} />
            </button>
          </div>
        ) : (
          <p className="text-xs text-[#64748B]">O secret é usado para assinar JWTs. Clique em "Revelar" para vê-lo.</p>
        )}
      </div>

      {/* Token List */}
      <div className="bg-white/[0.04] border border-white/[0.08] rounded-2xl p-4">
        <p className="text-[12px] font-semibold text-[#94A3B8] uppercase tracking-wider mb-3">Tokens</p>
        {isLoading ? (
          <p className="text-sm text-[#94A3B8]">Carregando...</p>
        ) : !tokens || tokens.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-8 text-center text-[#94A3B8]">
            <Key size={18} className="opacity-40" />
            <p className="text-[13px] font-medium">Nenhum token</p>
            <p className="text-[11px]">Crie um token para gerar um JWT</p>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {tokens.map(t => (
              <div key={t.id} className="flex items-center justify-between bg-black/20 rounded-xl px-4 py-3">
                <div className="flex flex-col gap-0.5 min-w-0">
                  <p className="text-sm font-semibold text-[#F8FAFC] truncate">{t.name}</p>
                  <p className="text-[11px] text-[#64748B]">
                    {t.expires_at ? `Expira ${new Date(t.expires_at).toLocaleDateString()}` : 'Nunca expira'}
                    {t.last_used_at && ` · Último uso ${new Date(t.last_used_at).toLocaleDateString()}`}
                  </p>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  {statusBadge(t)}
                  {!t.revoked_at && (
                    <button
                      onClick={() => revokeToken.mutate(t.id)}
                      className="p-1.5 rounded-lg hover:bg-white/[0.08] text-[#94A3B8] hover:text-red-400 transition-colors bg-transparent border-none cursor-pointer"
                    >
                      <X size={14} />
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Create Token Modal */}
      {showCreate && <CreateTokenModal appId={app.id} onClose={() => { setShowCreate(false); setCreatedToken(null) }} onCreated={(jwt) => { setCreatedToken(jwt); setShowCreate(false) }} />}
      {createdToken && <TokenRevealModal jwt={createdToken} onClose={() => setCreatedToken(null)} />}

      {/* Regenerate Confirm Modal */}
      {showRegenerateConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="bg-[#0F172A] border border-white/[0.08] rounded-2xl p-6 max-w-md w-full mx-4">
            <div className="flex items-center gap-3 mb-4">
              <AlertTriangle size={20} className="text-yellow-400" />
              <p className="text-[15px] font-bold text-[#F8FAFC]">Regenerar JWT Secret</p>
            </div>
            <p className="text-sm text-[#94A3B8] mb-6">
              Todos os tokens existentes serão imediatamente invalidados. Esta ação não pode ser desfeita.
            </p>
            <div className="flex items-center justify-end gap-3">
              <button onClick={() => setShowRegenerateConfirm(false)} className="px-4 py-2 rounded-full border border-white/[0.12] text-[13px] text-[#94A3B8] cursor-pointer bg-transparent hover:text-white transition-colors">
                Cancelar
              </button>
              <button
                onClick={() => {
                  regenerateSecret.mutate(undefined, {
                    onSuccess: (data) => {
                      setShowRegenerateConfirm(false)
                      setRevealedSecret(data.jwt_secret)
                    }
                  })
                }}
                disabled={regenerateSecret.isPending}
                className="px-4 py-2 rounded-full text-[13px] font-semibold text-white cursor-pointer disabled:opacity-50"
                style={{ background: "linear-gradient(to right, #EF4444, #DC2626)" }}
              >
                {regenerateSecret.isPending ? "Regenerando..." : "Confirmar Regeneração"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function CreateTokenModal({ appId, onClose, onCreated }: { appId: string; onClose: () => void; onCreated: (jwt: string) => void }) {
  const createToken = useCreateAppToken(appId)
  const [name, setName] = useState("")
  const [expiration, setExpiration] = useState("30d")
  const [error, setError] = useState<string | null>(null)

  async function handleCreate() {
    setError(null)
    try {
      const res = await createToken.mutateAsync({ name, expiration: expiration as any })
      onCreated(res.token)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Erro ao criar token")
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-[#0F172A] border border-white/[0.08] rounded-2xl p-6 max-w-md w-full mx-4">
        <p className="text-[15px] font-bold text-[#F8FAFC] mb-4">Novo Token</p>
        <div className="flex flex-col gap-3">
          <div>
            <Label className="text-[12px] font-medium text-[#94A3B8]">Nome</Label>
            <Input value={name} onChange={e => setName(e.target.value)} placeholder="Ex: Production frontend" className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] placeholder:text-white/30 brand-focus mt-1" />
          </div>
          <div>
            <Label className="text-[12px] font-medium text-[#94A3B8]">Expiração</Label>
            <select value={expiration} onChange={e => setExpiration(e.target.value)} className="h-10 rounded-md bg-white/[0.05] border border-white/[0.10] text-[#F8FAFC] brand-focus mt-1 w-full px-3">
              <option value="7d">7 dias</option>
              <option value="30d">30 dias</option>
              <option value="365d">365 dias</option>
              <option value="never">Nunca expira</option>
            </select>
          </div>
          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>
        <div className="flex items-center justify-end gap-3 mt-6">
          <button onClick={onClose} className="px-4 py-2 rounded-full border border-white/[0.12] text-[13px] text-[#94A3B8] cursor-pointer bg-transparent hover:text-white transition-colors">Cancelar</button>
          <button onClick={handleCreate} disabled={!name || createToken.isPending} className="px-4 py-2 rounded-full text-[13px] font-semibold text-white cursor-pointer disabled:opacity-50" style={{ background: "linear-gradient(to right, var(--brand-primary), var(--brand-secondary))" }}>
            {createToken.isPending ? "Criando..." : "Criar Token"}
          </button>
        </div>
      </div>
    </div>
  )
}

function TokenRevealModal({ jwt, onClose }: { jwt: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-[#0F172A] border border-white/[0.08] rounded-2xl p-6 max-w-lg w-full mx-4">
        <div className="flex items-center gap-3 mb-4">
          <Key size={20} className="text-[var(--brand-light)]" />
          <p className="text-[15px] font-bold text-[#F8FAFC]">Token Criado</p>
        </div>
        <p className="text-xs text-yellow-400 mb-3">Copie este token agora. Não será possível vê-lo novamente.</p>
        <div className="flex items-center gap-2 bg-black/30 rounded-xl px-4 py-3">
          <code className="text-sm text-[#B3D1FF] break-all font-mono flex-1 max-h-32 overflow-y-auto">{jwt}</code>
          <button
            onClick={() => { navigator.clipboard.writeText(jwt); setCopied(true) }}
            className="shrink-0 p-2 rounded-lg hover:bg-white/[0.08] text-[#94A3B8] hover:text-[#F8FAFC] transition-colors bg-transparent border-none cursor-pointer"
          >
            <Copy size={16} />
          </button>
        </div>
        {copied && <p className="text-[11px] text-emerald-400 mt-2">Copiado!</p>}
        <div className="flex justify-end mt-4">
          <button onClick={onClose} className="px-4 py-2 rounded-full text-[13px] font-semibold text-white cursor-pointer" style={{ background: "linear-gradient(to right, var(--brand-primary), var(--brand-secondary))" }}>Fechar</button>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Add tab trigger in AppDetailsPage**

Add to the TabsList:
```tsx
<TabsTrigger value="tokens" className="...">Tokens</TabsTrigger>
```

Add to the TabsContent area:
```tsx
<TabsContent value="tokens" className="mt-0">
  <TokensTab app={app} />
</TabsContent>
```

- [ ] **Step 4: Commit**

```bash
git add internal/dashboard/ui/src/pages/AppDetailsPage.tsx internal/dashboard/ui/src/lib/api.ts
git commit -m "feat: add tokens tab to app details page"
```

---

### Task 9: Tests

**Files:**
- Create: `internal/server/middleware_test.go` (appends to existing)
- Create: `internal/dashboard/handler_test.go` (appends to existing)
- Create: `internal/auth/handler_test.go` (appends to existing)

- [ ] **Step 1: Add middleware test for jti validation**

In `internal/server/middleware_test.go`, add tests:
- JWT with `token_type: "app_token"` and valid jti → 200
- JWT with `token_type: "app_token"` and revoked jti → 401
- JWT with `token_type: "app_token"` and expired jti → 401
- JWT without `token_type` (email auth) → 200 (unaffected)

- [ ] **Step 2: Add dashboard handler tests**

In `internal/dashboard/handler_test.go`, test:
- Create token on email-auth app → 400
- Create token on non-email app → 201 with JWT
- Revoke token → 200, subsequent use → 401
- Regenerate secret → new secret, all tokens revoked
- List tokens returns array

- [ ] **Step 3: Add auth handler test for token refresh**

In `internal/auth/handler_test.go`, test:
- Refresh with valid app token → 200 with new JWT
- Refresh with revoked token → 401
- Refresh with expired token → 401
- Refresh on email-auth app → 404

- [ ] **Step 4: Run all tests**

```bash
go test ./internal/server/... ./internal/dashboard/... ./internal/auth/... -v -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/middleware_test.go internal/dashboard/handler_test.go internal/auth/handler_test.go
git commit -m "test: add tests for app tokens feature"
```
