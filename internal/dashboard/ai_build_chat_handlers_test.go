package dashboard

// ai_build_chat_handlers_test.go — coverage for BuildChatTurn and
// GetBuildChatSession (T8), derived from spec.md's P2/P3 acceptance criteria
// (AIBC-07, AIBC-08, AIBC-12 through AIBC-18) and tasks.md's T8 Done-when
// list — not from reading the implementation. BuildChatConfirm (T9) and
// RestartBuildChatSession + route wiring (T10) add their own tests to this
// file in their own commits.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard/ai"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// aiBuildChatHandlerTestPool follows the same DSN/provision/truncate/
// NewHandler pattern as aiProviderHandlerTestPool (ai_provider_handlers_test.go)
// and aiBuildSessionsTestPool (ai_build_sessions_store_test.go), combined
// because BuildChatTurn/BuildChatConfirm need both the provider config and
// the session/message tables plus a real app-creating user.
func aiBuildChatHandlerTestPool(t *testing.T) (*db.Pool, *Handler, *DashboardUser) {
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
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	truncate := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.audit_log`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.ai_build_messages`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.ai_build_sessions`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.ai_providers`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.app_tables CASCADE`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.apps CASCADE`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.dashboard_users CASCADE`)
	}
	truncate()
	t.Cleanup(truncate)

	os.Setenv("DASHBOARD_BOOTSTRAP_SECRET", "test-secret-for-ai-build-chat-handlers")

	h := NewHandler(pool, registry.New(), zap.NewNop())
	user, err := CreateUser(ctx, pool, fmt.Sprintf("build-chat-handler-%d@example.com", time.Now().UnixNano()), "Chat User", "hash", "admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return pool, h, user
}

// withFakeAIModel swaps callAIModel for a fake for the duration of one test,
// restoring the real ai.CallModel afterward — avoids any dependency on the
// real OpenAI API or on internal/dashboard/ai's own private test seam.
func withFakeAIModel(t *testing.T, fake func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error)) {
	t.Helper()
	original := callAIModel
	callAIModel = fake
	t.Cleanup(func() { callAIModel = original })
}

func setOpenAIProvider(t *testing.T, pool *db.Pool, enabled bool) {
	t.Helper()
	_, err := UpsertAIProvider(context.Background(), pool, "openai", &aiProviderUpsertInput{
		Model:   "gpt-4o",
		APIKey:  "sk-test-key",
		Enabled: enabled,
	})
	if err != nil {
		t.Fatalf("UpsertAIProvider: %v", err)
	}
}

func buildChatTurnRequestFor(user *DashboardUser, content string) *http.Request {
	body, _ := json.Marshal(buildChatTurnRequest{Content: content})
	r := httptest.NewRequest(http.MethodPost, "/dashboard/api/ai/build-chat", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if user != nil {
		r = withUser(r, user)
	}
	return r
}

// AIBC-08: with no in_progress session, GetBuildChatSession creates one and
// returns it with an empty message history.
func TestGetBuildChatSession_CreatesNewSession(t *testing.T) {
	_, h, user := aiBuildChatHandlerTestPool(t)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/ai/build-chat/session", nil)
	req = withUser(req, user)
	w := httptest.NewRecorder()
	h.GetBuildChatSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got buildChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Session.Status != "in_progress" {
		t.Errorf("expected status in_progress, got %q", got.Session.Status)
	}
	if len(got.Messages) != 0 {
		t.Errorf("expected empty history for a freshly created session, got %d", len(got.Messages))
	}
}

// AIBC-07: opening the drawer with an existing in_progress session resumes
// its full message history instead of creating a new one.
func TestGetBuildChatSession_ResumesExistingHistory(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	if err := AppendMessage(ctx, pool, session.ID, "user", "I want a ticketing app", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/ai/build-chat/session", nil)
	req = withUser(req, user)
	w := httptest.NewRecorder()
	h.GetBuildChatSession(w, req)

	var got buildChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Session.ID != session.ID {
		t.Fatalf("expected the same session to be resumed, got %q vs %q", got.Session.ID, session.ID)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "I want a ticketing app" {
		t.Fatalf("expected resumed history to include the prior message, got %+v", got.Messages)
	}
}

// AIBC-12/13: sending a message persists it, calls the model, and returns
// (and persists) a message-shape assistant response when the model isn't
// ready to propose a plan yet.
func TestBuildChatTurn_MessageShapeTurn(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	setOpenAIProvider(t, pool, true)
	withFakeAIModel(t, func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error) {
		return ai.ChatTurnResult{Kind: "message", Content: "Do you need authentication?"}, nil
	})

	req := buildChatTurnRequestFor(user, "I want a ticketing app")
	w := httptest.NewRecorder()
	h.BuildChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got buildChatTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "message" || got.Content != "Do you need authentication?" {
		t.Fatalf("expected message-shape response, got %+v", got)
	}
	if got.Plan != nil {
		t.Fatal("expected no plan on a message-shape response")
	}

	session, messages, err := GetOrCreateInProgressSession(context.Background(), pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	if session == nil || len(messages) != 2 {
		t.Fatalf("expected 2 persisted messages (user + assistant), got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "I want a ticketing app" {
		t.Errorf("expected persisted user message, got %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "Do you need authentication?" {
		t.Errorf("expected persisted assistant message, got %+v", messages[1])
	}
}

// AIBC-14: when the model calls propose_app_plan, the turn returns and
// persists a plan-shape response with the plan JSON on the message row.
func TestBuildChatTurn_PlanShapeTurn(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	setOpenAIProvider(t, pool, true)
	plan := &ai.AppPlan{Name: "ticketing", Auth: true, Tables: []ai.PlanTable{{Name: "tickets", Columns: []ai.PlanColumn{{Name: "title", Type: "text"}}}}}
	withFakeAIModel(t, func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error) {
		return ai.ChatTurnResult{Kind: "plan", Plan: plan}, nil
	})

	req := buildChatTurnRequestFor(user, "a ticketing app with email auth")
	w := httptest.NewRecorder()
	h.BuildChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got buildChatTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "plan" {
		t.Fatalf("expected plan-shape response, got %+v", got)
	}
	if got.Plan == nil || got.Plan.Name != "ticketing" || len(got.Plan.Tables) != 1 || got.Plan.Tables[0].Name != "tickets" {
		t.Fatalf("expected the plan's fields to match the model's tool call, got %+v", got.Plan)
	}

	_, messages, err := GetOrCreateInProgressSession(context.Background(), pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(messages))
	}
	var decoded map[string]any
	if err := json.Unmarshal(messages[1].Plan, &decoded); err != nil {
		t.Fatalf("expected plan JSON persisted on the assistant message: %v", err)
	}
	if decoded["name"] != "ticketing" {
		t.Errorf("expected persisted plan name %q, got %+v", "ticketing", decoded)
	}
}

// AIBC-16: a model-call failure returns the fixed generic chat message
// (never the real error) and leaves only the user's own message persisted.
func TestBuildChatTurn_ModelFailureReturnsGenericMessage(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	setOpenAIProvider(t, pool, true)
	withFakeAIModel(t, func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error) {
		return ai.ChatTurnResult{}, fmt.Errorf("openai: rate limit exceeded for key sk-super-secret")
	})

	req := buildChatTurnRequestFor(user, "I want a ticketing app")
	w := httptest.NewRecorder()
	h.BuildChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (chat-visible error, not an HTTP error), got %d: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sk-super-secret")) {
		t.Fatal("expected the real error to never leak into the HTTP response")
	}
	var got buildChatTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "message" || got.Content != genericAIChatError {
		t.Fatalf("expected the fixed generic message, got %+v", got)
	}

	_, messages, err := GetOrCreateInProgressSession(context.Background(), pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected only the user's own message persisted after a model failure, got %d", len(messages))
	}
}

// Spec Edge Cases: no provider configured (or enabled=false) is treated
// identically to a model-call failure — generic chat message, no crash.
func TestBuildChatTurn_UnconfiguredProviderReturnsGenericMessage(t *testing.T) {
	_, h, user := aiBuildChatHandlerTestPool(t)
	// No setOpenAIProvider call — provider row doesn't exist at all.

	req := buildChatTurnRequestFor(user, "I want a ticketing app")
	w := httptest.NewRecorder()
	h.BuildChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got buildChatTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "message" || got.Content != genericAIChatError {
		t.Fatalf("expected the fixed generic message for an unconfigured provider, got %+v", got)
	}
}

// AIBC-18: enabled=false on the provider produces the same "unconfigured"
// behavior as no provider row at all, so the frontend can gate on GET
// /api/ai-providers/openai's enabled field before ever opening the drawer.
func TestBuildChatTurn_DisabledProviderReturnsGenericMessage(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	setOpenAIProvider(t, pool, false)

	req := buildChatTurnRequestFor(user, "I want a ticketing app")
	w := httptest.NewRecorder()
	h.BuildChatTurn(w, req)

	var got buildChatTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "message" || got.Content != genericAIChatError {
		t.Fatalf("expected the fixed generic message for a disabled provider, got %+v", got)
	}
}

// AIBC-17: the read-tool invoker resolves list_apps/get_app_schema against
// the real ListAppsForUser/GetAppSchemaForUser paths — never fabricating
// schema for an app it hasn't looked up. Tested directly against the
// closure (not through a fake model round-trip) since that's the unit the
// handler itself owns.
func TestBuildChatReadToolInvoker_ListAppsAndGetAppSchema(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	created, err := h.CreateAppForUser(ctx, user, AppRequestBody{Name: "helpdesk"}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, user, created.ID, TableRequestBody{Name: "tickets"}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	invoke := h.buildChatReadToolInvoker(user)

	listResult, err := invoke(ctx, "list_apps", nil)
	if err != nil {
		t.Fatalf("list_apps: %v", err)
	}
	var apps []AppRow
	if err := json.Unmarshal(listResult, &apps); err != nil {
		t.Fatalf("unmarshal list_apps result: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "helpdesk" {
		t.Fatalf("expected list_apps to return the real created app, got %+v", apps)
	}

	schemaArgs, _ := json.Marshal(map[string]string{"app_name": "helpdesk"})
	schemaResult, err := invoke(ctx, "get_app_schema", schemaArgs)
	if err != nil {
		t.Fatalf("get_app_schema: %v", err)
	}
	var schema AppSchema
	if err := json.Unmarshal(schemaResult, &schema); err != nil {
		t.Fatalf("unmarshal get_app_schema result: %v", err)
	}
	if schema.AppName != "helpdesk" || len(schema.Tables) != 1 || schema.Tables[0].Name != "tickets" {
		t.Fatalf("expected get_app_schema to return the real schema, got %+v", schema)
	}

	unknownArgs, _ := json.Marshal(map[string]string{"app_name": "does-not-exist"})
	unknownResult, err := invoke(ctx, "get_app_schema", unknownArgs)
	if err != nil {
		t.Fatalf("get_app_schema (unknown app): %v", err)
	}
	var errPayload map[string]string
	if err := json.Unmarshal(unknownResult, &errPayload); err != nil {
		t.Fatalf("unmarshal unknown-app result: %v", err)
	}
	if errPayload["error"] == "" {
		t.Fatal("expected an error payload for an unknown app_name, not a fabricated schema")
	}

	_ = pool
}
