package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-core/internal/config"
	"github.com/zeeplabs/zeep-core/internal/db"
	"github.com/zeeplabs/zeep-core/internal/registry"
)

const (
	testSchema = "auth_test_app"
	testApp    = "auth_test_app"
	testSecret = "test-jwt-secret-auth"
)

var (
	testPool *db.Pool
	testReg  *registry.Registry
	testH    *Handler
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		os.Exit(0)
	}

	ctx := context.Background()
	var err error
	testPool, err = db.New(ctx, dsn)
	if err != nil {
		panic("auth TestMain: failed to connect: " + err.Error())
	}
	defer testPool.Close()

	setup := []string{
		"DROP SCHEMA IF EXISTS " + testSchema + " CASCADE",
		"CREATE SCHEMA " + testSchema,
		`CREATE TABLE ` + testSchema + `."_auth_users" (
			"id"            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			"email"         TEXT        NOT NULL UNIQUE,
			"password_hash" TEXT        NOT NULL,
			"name"          TEXT,
			"avatar_url"    TEXT,
			"created_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
			"updated_at"    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE ` + testSchema + `."_auth_sessions" (
			"id"            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			"user_id"       UUID        NOT NULL REFERENCES ` + testSchema + `."_auth_users"("id") ON DELETE CASCADE,
			"refresh_token" TEXT        NOT NULL UNIQUE,
			"expires_at"    TIMESTAMPTZ NOT NULL,
			"created_at"    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			panic("auth TestMain: setup failed: " + err.Error())
		}
	}

	testReg = registry.New()
	_ = testReg.Load(&config.Config{
		Apps: []config.AppConfig{
			{
				Name: testApp,
				Auth: config.AuthConfig{
					JWTSecret: testSecret,
					Providers: config.AuthProviders{Email: true},
				},
			},
		},
	})

	testH = New(testPool, testReg)

	code := m.Run()

	_, _ = testPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+testSchema+" CASCADE")
	os.Exit(code)
}

// buildAuthRouter mounts auth routes the same way server.go does.
func buildAuthRouter() http.Handler {
	r := chi.NewRouter()
	r.Route("/{app}/auth", func(r chi.Router) {
		r.With(testH.RateLimit).Post("/register", testH.Register)
		r.With(testH.RateLimit).Post("/login", testH.Login)
		r.Post("/refresh", testH.Refresh)
		r.With(authJWTMiddleware()).Post("/logout", testH.Logout)
		r.With(authJWTMiddleware()).Get("/me", testH.Me)
		r.With(authJWTMiddleware()).Put("/me", testH.UpdateMe)
	})
	return r
}

// authJWTMiddleware validates JWT and injects AuthUser — mirrors server.AuthJWTMiddleware.
func authJWTMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			appName := chi.URLParam(r, "app")
			app, ok := testReg.Get(appName)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			raw := strings.TrimPrefix(header, "Bearer ")
			claims, err := ParseJWT([]byte(app.Config.Auth.JWTSecret), raw)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			ctx := WithUser(r.Context(), &AuthUser{
				ID:    claims.Subject,
				Email: claims.Email,
				App:   claims.App,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func jsonBody(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

func decodeJSON(t *testing.T, body *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(body).Decode(&m); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return m
}

// ----------------------------------------------------------------------------
// AC: POST /{app}/auth/register creates user and returns JWT
// ----------------------------------------------------------------------------

func TestRegisterReturns201WithToken(t *testing.T) {
	router := buildAuthRouter()

	req := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/register", jsonBody(map[string]any{
		"email":    "register@test.com",
		"password": "password123",
		"name":     "Test User",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	m := decodeJSON(t, rec.Body)
	if m["token"] == "" {
		t.Fatal("token missing in register response")
	}
}

func TestRegisterDuplicateEmail409(t *testing.T) {
	router := buildAuthRouter()

	body := map[string]any{"email": "dup@test.com", "password": "pass"}

	req1 := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/register", jsonBody(body))
	req1.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/register", jsonBody(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestRegisterMissingFields400(t *testing.T) {
	router := buildAuthRouter()

	req := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/register", jsonBody(map[string]any{
		"email": "nopw@test.com",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// AC: POST /{app}/auth/login validates credentials and returns JWT + refresh
// ----------------------------------------------------------------------------

func TestLoginReturnsTokenAndRefresh(t *testing.T) {
	router := buildAuthRouter()

	regReq := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/register", jsonBody(map[string]any{
		"email": "login@test.com", "password": "secret",
	}))
	regReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), regReq)

	req := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/login", jsonBody(map[string]any{
		"email": "login@test.com", "password": "secret",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := decodeJSON(t, rec.Body)
	if m["token"] == "" {
		t.Fatal("token missing")
	}
	if m["refresh_token"] == "" {
		t.Fatal("refresh_token missing")
	}
}

func TestLoginWrongPassword401(t *testing.T) {
	router := buildAuthRouter()

	regReq := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/register", jsonBody(map[string]any{
		"email": "wrongpw@test.com", "password": "correct",
	}))
	regReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), regReq)

	req := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/login", jsonBody(map[string]any{
		"email": "wrongpw@test.com", "password": "wrong",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// AC: POST /{app}/auth/refresh rotates refresh token and emits new JWT
// ----------------------------------------------------------------------------

func TestRefreshRotatesToken(t *testing.T) {
	router := buildAuthRouter()

	regReq := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/register", jsonBody(map[string]any{
		"email": "refresh@test.com", "password": "pass",
	}))
	regReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), regReq)

	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/login", jsonBody(map[string]any{
		"email": "refresh@test.com", "password": "pass",
	}))
	loginReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(loginRec, loginReq)

	loginData := decodeJSON(t, loginRec.Body)
	oldRefresh := loginData["refresh_token"].(string)

	req := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/refresh", jsonBody(map[string]any{
		"refresh_token": oldRefresh,
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := decodeJSON(t, rec.Body)
	if m["token"] == "" {
		t.Fatal("token missing after refresh")
	}
	if m["refresh_token"] == oldRefresh {
		t.Fatal("refresh_token was not rotated")
	}
}

func TestRefreshInvalidToken401(t *testing.T) {
	router := buildAuthRouter()

	req := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/refresh", jsonBody(map[string]any{
		"refresh_token": "nonexistent-token",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// AC: Token inválido em endpoint de dados retorna 401
// ----------------------------------------------------------------------------

func TestInvalidJWTReturns401(t *testing.T) {
	r := chi.NewRouter()
	r.With(authJWTMiddleware()).Get("/{app}/items/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/"+testApp+"/items/", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// AC: Apps sem providers.email não expõem endpoints auth
// ----------------------------------------------------------------------------

func TestNoEmailProviderReturns404(t *testing.T) {
	noAuthReg := registry.New()
	_ = noAuthReg.Load(&config.Config{
		Apps: []config.AppConfig{
			{
				Name: "noauth",
				Auth: config.AuthConfig{JWTSecret: "s"},
				// Providers.Email == false (zero value)
			},
		},
	})
	h := New(testPool, noAuthReg)

	r := chi.NewRouter()
	r.Route("/{app}/auth", func(r chi.Router) {
		r.Post("/register", h.Register)
		r.Post("/login", h.Login)
	})

	req := httptest.NewRequest(http.MethodPost, "/noauth/auth/register", jsonBody(map[string]any{
		"email": "x@x.com", "password": "pw",
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for app without email provider, got %d", rec.Code)
	}
}

// ----------------------------------------------------------------------------
// AC: tabelas _auth_* não aparecem como tabelas CRUD nem no registry
// ----------------------------------------------------------------------------

func TestAuthTablesNotExposedAsCRUD(t *testing.T) {
	app, _ := testReg.Get(testApp)
	if _, ok := app.Tables["_auth_users"]; ok {
		t.Fatal("_auth_users must not be in app.Tables")
	}
	if _, ok := app.Tables["_auth_sessions"]; ok {
		t.Fatal("_auth_sessions must not be in app.Tables")
	}
}

// ----------------------------------------------------------------------------
// AC: GET /{app}/auth/me returns current user
// ----------------------------------------------------------------------------

func TestMeReturnsUser(t *testing.T) {
	router := buildAuthRouter()

	regRec := httptest.NewRecorder()
	regReq := httptest.NewRequest(http.MethodPost, "/"+testApp+"/auth/register", jsonBody(map[string]any{
		"email": "me@test.com", "password": "pass", "name": "Me User",
	}))
	regReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(regRec, regReq)

	regData := decodeJSON(t, regRec.Body)
	token := regData["token"].(string)

	req := httptest.NewRequest(http.MethodGet, "/"+testApp+"/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	m := decodeJSON(t, rec.Body)
	if m["email"] != "me@test.com" {
		t.Fatalf("email mismatch: %v", m["email"])
	}
}
