package dashboard

// ai_provider_handlers_test.go — HTTP-level coverage for GET/PUT on the
// global AI provider config endpoint. Derived from spec.md's P1 acceptance
// criteria (AIBC-01/02/04/06) and tasks.md's T4 Done-when list — not from
// reading the implementation. Reuses mustCreateSuperadmin/
// mustCreateRegularUser/withUser/withCtx already defined for
// github_config_test.go/apps_handler_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

func aiProviderHandlerTestPool(t *testing.T) (*db.Pool, *Handler) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision schema: %v", err)
	}

	truncate := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.audit_log`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.ai_providers`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.dashboard_users CASCADE`)
	}
	truncate()
	t.Cleanup(truncate)

	os.Setenv("DASHBOARD_BOOTSTRAP_SECRET", "test-secret-for-ai-provider-handlers")

	h := NewHandler(pool, registry.New(), zap.NewNop())
	return pool, h
}

// requestWithProvider builds an httptest request carrying the given user in
// context and "provider" set as a chi URL param, so the handler's
// chi.URLParam(r, "provider") call resolves correctly outside a real router.
func requestWithProvider(method, path string, body string, user *DashboardUser, provider string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if user != nil {
		r = withUser(r, user)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider", provider)
	r = r.WithContext(withCtx(r, rctx))
	return r
}

// AIBC-02: a non-superadmin PUT is rejected with 403 and never reaches the
// store — verified by checking GetAIProvider still reports has_key: false
// afterward.
func TestUpsertAIProviderConfig_NonSuperadminForbiddenNoMutation(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	regular := mustCreateRegularUser(t, pool)

	req := requestWithProvider(http.MethodPut, "/dashboard/api/ai-providers/openai",
		`{"model":"gpt-4o","api_key":"sk-should-not-be-stored"}`, regular, "openai")
	w := httptest.NewRecorder()
	h.UpsertAIProviderConfig(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	resp, err := GetAIProvider(context.Background(), pool, "openai")
	if err != nil {
		t.Fatalf("GetAIProvider: %v", err)
	}
	if resp.HasKey {
		t.Fatal("expected no store mutation after a forbidden PUT, but has_key is true")
	}
}

// An empty/blank model is rejected with 400 before the store is touched —
// found by the pre-v1.6.0 Opus review (v1.5.0..HEAD, finding M4): the
// frontend's model select can produce "" (picking "custom" without typing
// one), and saving it used to succeed silently, leaving every chat turn
// afterward failing with a generic, undiagnosable error.
func TestUpsertAIProviderConfig_EmptyModelRejected(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	for _, model := range []string{"", "   "} {
		body, err := json.Marshal(map[string]any{"model": model, "api_key": "sk-real-key-abc", "enabled": true})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req := requestWithProvider(http.MethodPut, "/dashboard/api/ai-providers/openai", string(body), super, "openai")
		w := httptest.NewRecorder()
		h.UpsertAIProviderConfig(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("model=%q: expected 400, got %d: %s", model, w.Code, w.Body.String())
		}
	}

	resp, err := GetAIProvider(context.Background(), pool, "openai")
	if err != nil {
		t.Fatalf("GetAIProvider: %v", err)
	}
	if resp.HasKey {
		t.Fatal("expected no store mutation after a rejected empty-model PUT, but has_key is true")
	}
}

// AIBC-01/AIBC-04: a superadmin PUT with key+model succeeds, and a
// subsequent GET reflects has_key: true with no key material in the body.
func TestUpsertAIProviderConfig_SuperadminSucceedsAndGetReflectsHasKey(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	putReq := requestWithProvider(http.MethodPut, "/dashboard/api/ai-providers/openai",
		`{"model":"gpt-4o","api_key":"sk-real-key-abc","enabled":true}`, super, "openai")
	putW := httptest.NewRecorder()
	h.UpsertAIProviderConfig(putW, putReq)

	if putW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putW.Code, putW.Body.String())
	}
	if bytesContain(putW.Body.Bytes(), "sk-real-key-abc") {
		t.Fatal("expected the PUT response to never echo the plaintext key")
	}

	getReq := requestWithProvider(http.MethodGet, "/dashboard/api/ai-providers/openai", "", super, "openai")
	getW := httptest.NewRecorder()
	h.GetAIProviderConfig(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	var got AIProviderResponse
	if err := json.Unmarshal(getW.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}
	if !got.HasKey {
		t.Error("expected has_key: true")
	}
	if got.Model != "gpt-4o" {
		t.Errorf("expected model %q, got %q", "gpt-4o", got.Model)
	}
	if bytesContain(getW.Body.Bytes(), "sk-real-key-abc") {
		t.Fatal("expected the GET response to never carry the plaintext key")
	}
}

// AIBC-03 (HTTP level): a model-only PUT (no api_key field) preserves the
// stored key, and the app can still resolve it afterward.
func TestUpsertAIProviderConfig_ModelOnlyUpdatePreservesKey(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	initial := requestWithProvider(http.MethodPut, "/dashboard/api/ai-providers/openai",
		`{"model":"gpt-4o","api_key":"sk-preserve-through-http","enabled":true}`, super, "openai")
	initialW := httptest.NewRecorder()
	h.UpsertAIProviderConfig(initialW, initial)
	if initialW.Code != http.StatusOK {
		t.Fatalf("initial PUT: expected 200, got %d", initialW.Code)
	}

	modelOnly := requestWithProvider(http.MethodPut, "/dashboard/api/ai-providers/openai",
		`{"model":"gpt-4o-mini","enabled":true}`, super, "openai")
	modelOnlyW := httptest.NewRecorder()
	h.UpsertAIProviderConfig(modelOnlyW, modelOnly)
	if modelOnlyW.Code != http.StatusOK {
		t.Fatalf("model-only PUT: expected 200, got %d: %s", modelOnlyW.Code, modelOnlyW.Body.String())
	}

	_, key, err := resolveDecryptedAIProviderKey(context.Background(), pool, "openai")
	if err != nil {
		t.Fatalf("resolveDecryptedAIProviderKey: %v", err)
	}
	if key != "sk-preserve-through-http" {
		t.Fatalf("expected preserved key, got %q", key)
	}
}

// AIBC-06: PUT to gemini/claude returns 501 and never persists anything.
func TestUpsertAIProviderConfig_GeminiClaudeReturn501NoPersistence(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	for _, provider := range []string{"gemini", "claude"} {
		req := requestWithProvider(http.MethodPut, "/dashboard/api/ai-providers/"+provider,
			`{"model":"some-model","api_key":"sk-should-not-persist"}`, super, provider)
		w := httptest.NewRecorder()
		h.UpsertAIProviderConfig(w, req)

		if w.Code != http.StatusNotImplemented {
			t.Fatalf("%s: expected 501, got %d: %s", provider, w.Code, w.Body.String())
		}

		resp, err := GetAIProvider(context.Background(), pool, provider)
		if err != nil {
			t.Fatalf("GetAIProvider(%q): %v", provider, err)
		}
		if resp.HasKey {
			t.Fatalf("%s: expected no persistence after a 501 response, but has_key is true", provider)
		}
	}
}

// Unauthenticated requests to both routes are rejected before any handler
// logic runs — this test drives the handlers directly (no session cookie
// set in context) since RequireAuth's own coverage is exercised elsewhere;
// here we confirm the handlers' own UserFromContext guard.
func TestAIProviderConfig_UnauthenticatedRequestsRejected(t *testing.T) {
	_, h := aiProviderHandlerTestPool(t)

	getReq := requestWithProvider(http.MethodGet, "/dashboard/api/ai-providers/openai", "", nil, "openai")
	getW := httptest.NewRecorder()
	h.GetAIProviderConfig(getW, getReq)
	if getW.Code != http.StatusUnauthorized {
		t.Errorf("GET: expected 401, got %d", getW.Code)
	}

	putReq := requestWithProvider(http.MethodPut, "/dashboard/api/ai-providers/openai",
		`{"model":"gpt-4o","api_key":"sk-x"}`, nil, "openai")
	putW := httptest.NewRecorder()
	h.UpsertAIProviderConfig(putW, putReq)
	if putW.Code != http.StatusUnauthorized {
		t.Errorf("PUT: expected 401, got %d", putW.Code)
	}
}

func bytesContain(haystack []byte, needle string) bool {
	return bytes.Contains(haystack, []byte(needle))
}
