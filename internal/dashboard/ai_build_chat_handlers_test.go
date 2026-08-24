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

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/zeeplabs/zeep-orbit/internal/config"
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
	t.Cleanup(pool.Close)
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

// aiBuildChatHandlerTestPoolWithObservedLogger is aiBuildChatHandlerTestPool
// but wires an observer.New logger instead of zap.NewNop, so a test can
// assert on what got logged (AIBC-16's "logs the real error server-side"
// clause — zap.NewNop discards everything, giving no observation point).
func aiBuildChatHandlerTestPoolWithObservedLogger(t *testing.T) (*db.Pool, *Handler, *DashboardUser, *observer.ObservedLogs) {
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

	core, observed := observer.New(zap.ErrorLevel)
	h := NewHandler(pool, registry.New(), zap.New(core))
	user, err := CreateUser(ctx, pool, fmt.Sprintf("build-chat-handler-obs-%d@example.com", time.Now().UnixNano()), "Chat User", "hash", "admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return pool, h, user, observed
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

	// AIBC-17: a plan-shape turn alone must never mutate — no app named
	// after the plan exists until an explicit confirm (T9) runs.
	apps, err := ListAppsForUser(context.Background(), pool, user)
	if err != nil {
		t.Fatalf("ListAppsForUser: %v", err)
	}
	for _, a := range apps {
		if a.Name == "ticketing" {
			t.Fatalf("expected no app created from a plan-shape BuildChatTurn alone, found %+v", a)
		}
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

// AIBC-16 (second half): a model-call failure is logged server-side at
// Error level with the session ID, not just swallowed — the leak-prevention
// half is covered by TestBuildChatTurn_ModelFailureReturnsGenericMessage
// above; this asserts the diagnosability half using an observed logger
// (zap.NewNop, used elsewhere in this file, discards everything and can't
// prove a log call happened).
func TestBuildChatTurn_ModelFailureLogsRealErrorServerSide(t *testing.T) {
	pool, h, user, observed := aiBuildChatHandlerTestPoolWithObservedLogger(t)
	setOpenAIProvider(t, pool, true)
	withFakeAIModel(t, func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error) {
		return ai.ChatTurnResult{}, fmt.Errorf("openai: rate limit exceeded for key sk-super-secret")
	})

	req := buildChatTurnRequestFor(user, "I want a ticketing app")
	w := httptest.NewRecorder()
	h.BuildChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	entries := observed.All()
	var found bool
	for _, e := range entries {
		if e.Level != zap.ErrorLevel {
			continue
		}
		for _, f := range e.Context {
			if f.Key == "session_id" && f.String != "" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected an Error-level log entry carrying session_id after a model-call failure, got %+v", entries)
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

// confirmRequestFor builds an httptest request carrying user in context and
// "session_id" set as a chi URL param, so chi.URLParam(r, "session_id")
// resolves outside a real router — same idiom as requestWithProvider
// (ai_provider_handlers_test.go). body is sent verbatim (even garbage) to
// prove BuildChatConfirm never reads a plan out of the request body
// (AIBC-24).
func confirmRequestFor(user *DashboardUser, sessionID string, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(http.MethodPost, "/dashboard/api/ai/build-chat/"+sessionID+"/confirm", bytes.NewReader([]byte(body)))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(http.MethodPost, "/dashboard/api/ai/build-chat/"+sessionID+"/confirm", nil)
	}
	if user != nil {
		r = withUser(r, user)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("session_id", sessionID)
	r = r.WithContext(withCtx(r, rctx))
	return r
}

// persistProposedPlan appends an assistant message carrying plan as its
// plan_json — the only way BuildChatConfirm ever learns of a plan (it never
// reads one from the confirm request body).
func persistProposedPlan(t *testing.T, pool *db.Pool, sessionID string, plan *ai.AppPlan) {
	t.Helper()
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := AppendMessage(context.Background(), pool, sessionID, "assistant", "", planJSON); err != nil {
		t.Fatalf("persistProposedPlan: %v", err)
	}
}

// AIBC-19/AIBC-21: a valid 2-table plan creates the app and both tables,
// completes the session with created_app_id set, and records the mutation's
// audit-log origin as "ai_chat".
func TestBuildChatConfirm_FullSuccessRecordsAiChatOrigin(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	plan := &ai.AppPlan{
		Name: "ticketing",
		Auth: true,
		Tables: []ai.PlanTable{
			{Name: "tickets", Columns: []ai.PlanColumn{{Name: "title", Type: "text"}}},
			{Name: "comments", Columns: []ai.PlanColumn{{Name: "body", Type: "text"}}},
		},
	}
	persistProposedPlan(t, pool, session.ID, plan)

	req := confirmRequestFor(user, session.ID, "")
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got buildChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.App == nil || got.App.Name != "ticketing" || !got.App.AuthEmailEnabled {
		t.Fatalf("expected the created app to match the plan (name, auth), got %+v", got.App)
	}
	if len(got.App.Tables) != 2 {
		t.Fatalf("expected both tables created, got %d", len(got.App.Tables))
	}

	finalSession, _, err := loadOwnedBuildChatSession(ctx, pool, session.ID, user.ID)
	if err != nil {
		t.Fatalf("loadOwnedBuildChatSession: %v", err)
	}
	if finalSession.Status != "completed" {
		t.Errorf("expected session status completed, got %q", finalSession.Status)
	}
	if finalSession.CreatedAppID == nil || *finalSession.CreatedAppID != got.App.ID {
		t.Errorf("expected created_app_id %q, got %v", got.App.ID, finalSession.CreatedAppID)
	}

	var origin string
	if err := pool.QueryRow(ctx,
		`SELECT ip_address FROM zeep_system.audit_log WHERE resource_id = $1 AND action = 'app.create'`, got.App.ID,
	).Scan(&origin); err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if origin != "ai_chat" {
		t.Errorf("expected audit origin %q, got %q", "ai_chat", origin)
	}
}

// AIBC-24: BuildChatConfirm never accepts a free-form/client-supplied plan —
// only the structured shape the server itself persisted from a
// propose_app_plan tool call. A garbage request body is ignored entirely; a
// session with no proposed plan yet is rejected regardless of what the body
// contains.
func TestBuildChatConfirm_IgnoresRequestBodyNoStoredPlanRejected(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}

	req := confirmRequestFor(user, session.ID, `{"name":"totally-not-proposed","tables":[{"name":"evil"}],"auth":false}`)
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (no proposed plan to confirm), got %d: %s", w.Code, w.Body.String())
	}

	apps, err := ListApps(ctx, pool, user)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	for _, a := range apps {
		if a.Name == "totally-not-proposed" {
			t.Fatal("expected the request body to never be treated as a plan, but the app was created")
		}
	}
}

// Spec Edge Cases: a table name colliding with a reserved-looking name
// (e.g. "_auth_users") is rejected before any mutation — no app, no table.
func TestBuildChatConfirm_ReservedTableNameRejectedBeforeAnyMutation(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	plan := &ai.AppPlan{Name: "widgets", Tables: []ai.PlanTable{{Name: "_auth_users"}}}
	persistProposedPlan(t, pool, session.ID, plan)

	req := confirmRequestFor(user, session.ID, "")
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	apps, err := ListApps(ctx, pool, user)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	for _, a := range apps {
		if a.Name == "widgets" {
			t.Fatal("expected no app to be created when a table name is rejected up front")
		}
	}
}

// AIBC-22: a partial failure (app created, table 2 fails validation) leaves
// the session in_progress with created_app_id already set, and table 1
// remains created (no rollback).
func TestBuildChatConfirm_PartialFailureLeavesSessionInProgress(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	plan := &ai.AppPlan{
		Name: "partial-app",
		Tables: []ai.PlanTable{
			{Name: "tickets", Columns: []ai.PlanColumn{{Name: "title", Type: "text"}}},
			// "1bad" is an invalid column name (must start with a letter) —
			// fails validateTableInput inside CreateAppTableForUser, after
			// table 1 has already been created.
			{Name: "comments", Columns: []ai.PlanColumn{{Name: "1bad", Type: "text"}}},
		},
	}
	persistProposedPlan(t, pool, session.ID, plan)

	req := confirmRequestFor(user, session.ID, "")
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 from the invalid column name, got %d: %s", w.Code, w.Body.String())
	}

	finalSession, _, err := loadOwnedBuildChatSession(ctx, pool, session.ID, user.ID)
	if err != nil {
		t.Fatalf("loadOwnedBuildChatSession: %v", err)
	}
	if finalSession.Status != "in_progress" {
		t.Fatalf("expected session to remain in_progress after a partial failure, got %q", finalSession.Status)
	}
	if finalSession.CreatedAppID == nil || *finalSession.CreatedAppID == "" {
		t.Fatal("expected created_app_id to already be set from the successful app-creation step")
	}

	app, _, err := GetApp(ctx, pool, *finalSession.CreatedAppID, user)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if len(app.Tables) != 1 || app.Tables[0].Name != "tickets" {
		t.Fatalf("expected table 1 to remain created (no rollback), got %+v", app.Tables)
	}
}

// AIBC-23: retrying confirm after a partial failure (fixed plan on the next
// attempt requires no client input — the same stored plan is reused) skips
// the table that already exists and creates only the missing one, without
// erroring as a duplicate.
func TestBuildChatConfirm_RetryAfterPartialFailureSkipsExistingTable(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	plan := &ai.AppPlan{
		Name: "retry-app",
		Tables: []ai.PlanTable{
			{Name: "tickets", Columns: []ai.PlanColumn{{Name: "title", Type: "text"}}},
			{Name: "comments", Columns: []ai.PlanColumn{{Name: "body", Type: "text"}}},
		},
	}
	persistProposedPlan(t, pool, session.ID, plan)

	// First attempt: create the app and table 1 directly (bypassing confirm)
	// to fabricate the exact "app created, table 1 exists, table 2 doesn't"
	// partial state without depending on a second, independently-failing
	// column — SetSessionCreatedApp is the same call confirm itself makes
	// right after CreateAppForUser succeeds (design.md Tech Decisions).
	app, err := h.CreateAppForUser(ctx, user, AppRequestBody{Name: plan.Name}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, user, app.ID, TableRequestBody{Name: "tickets", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}}}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	if err := SetSessionCreatedApp(ctx, pool, session.ID, app.ID); err != nil {
		t.Fatalf("SetSessionCreatedApp: %v", err)
	}

	req := confirmRequestFor(user, session.ID, "")
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected the retry to succeed, got %d: %s", w.Code, w.Body.String())
	}
	var got buildChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.App.Tables) != 2 {
		t.Fatalf("expected exactly 2 tables (no duplicate of the pre-existing one), got %d: %+v", len(got.App.Tables), got.App.Tables)
	}
	seen := map[string]int{}
	for _, tbl := range got.App.Tables {
		seen[tbl.Name]++
	}
	if seen["tickets"] != 1 || seen["comments"] != 1 {
		t.Fatalf("expected exactly one of each table, got %+v", seen)
	}
}

// AIBC-23 (design.md Risks & Concerns): a table that was physically
// provisioned but whose metadata row never got persisted (simulated by
// calling the provisioner directly, bypassing InsertAppTable) is NOT
// treated as "already exists" by a fresh GetApp check — CreateAppTableForUser
// is retried for it and self-heals via CREATE TABLE IF NOT EXISTS, rather
// than erroring as a duplicate.
func TestBuildChatConfirm_RetryAfterMetadataWriteFailureSelfHeals(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	plan := &ai.AppPlan{
		Name:   "self-heal-app",
		Tables: []ai.PlanTable{{Name: "tickets", Columns: []ai.PlanColumn{{Name: "title", Type: "text"}}}},
	}
	persistProposedPlan(t, pool, session.ID, plan)

	app, err := h.CreateAppForUser(ctx, user, AppRequestBody{Name: plan.Name}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if err := SetSessionCreatedApp(ctx, pool, session.ID, app.ID); err != nil {
		t.Fatalf("SetSessionCreatedApp: %v", err)
	}

	// Physically provision "tickets" without ever inserting its app_tables
	// metadata row — the exact "provisioned but metadata write failed"
	// state design.md's Risks & Concerns flags.
	cfg := buildAppConfig(app)
	cfg.Tables = []config.TableConfig{{Name: "tickets", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}}}}
	if _, err := h.prov.Apply(ctx, &config.Config{Apps: []config.AppConfig{cfg}}); err != nil {
		t.Fatalf("provisioner.Apply: %v", err)
	}

	preRetry, _, err := GetApp(ctx, pool, app.ID, user)
	if err != nil {
		t.Fatalf("GetApp (pre-retry): %v", err)
	}
	if len(preRetry.Tables) != 0 {
		t.Fatalf("expected no metadata row yet for the physically-provisioned table, got %+v", preRetry.Tables)
	}

	req := confirmRequestFor(user, session.ID, "")
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected the retry to self-heal and succeed, got %d: %s", w.Code, w.Body.String())
	}
	var got buildChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.App.Tables) != 1 || got.App.Tables[0].Name != "tickets" {
		t.Fatalf("expected the table's metadata to now be persisted exactly once, got %+v", got.App.Tables)
	}
}

// AIBC-23: when all of a plan's tables already exist (e.g. a retry after
// CompleteSession itself failed to run on a prior attempt) confirm skips
// every table and still completes successfully — "idempotent retry after
// full early success".
func TestBuildChatConfirm_AllTablesAlreadyExistStillCompletes(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	plan := &ai.AppPlan{
		Name:   "already-done-app",
		Tables: []ai.PlanTable{{Name: "tickets", Columns: []ai.PlanColumn{{Name: "title", Type: "text"}}}},
	}
	persistProposedPlan(t, pool, session.ID, plan)

	app, err := h.CreateAppForUser(ctx, user, AppRequestBody{Name: plan.Name}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, user, app.ID, TableRequestBody{Name: "tickets", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}}}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	if err := SetSessionCreatedApp(ctx, pool, session.ID, app.ID); err != nil {
		t.Fatalf("SetSessionCreatedApp: %v", err)
	}

	req := confirmRequestFor(user, session.ID, "")
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	finalSession, _, err := loadOwnedBuildChatSession(ctx, pool, session.ID, user.ID)
	if err != nil {
		t.Fatalf("loadOwnedBuildChatSession: %v", err)
	}
	if finalSession.Status != "completed" {
		t.Fatalf("expected status completed, got %q", finalSession.Status)
	}
}

// AIBC-20: a user whose write permission on the already-created app was
// revoked between opening the chat and confirming is rejected with 403
// before any further mutation, identically to the REST/MCP behavior.
func TestBuildChatConfirm_RevokedWritePermissionForbidden(t *testing.T) {
	pool, h, user := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, user.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	plan := &ai.AppPlan{
		Name:   "revoked-app",
		Tables: []ai.PlanTable{{Name: "tickets", Columns: []ai.PlanColumn{{Name: "title", Type: "text"}}}},
	}
	persistProposedPlan(t, pool, session.ID, plan)

	app, err := h.CreateAppForUser(ctx, user, AppRequestBody{Name: plan.Name}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if err := SetSessionCreatedApp(ctx, pool, session.ID, app.ID); err != nil {
		t.Fatalf("SetSessionCreatedApp: %v", err)
	}

	// Simulate the app-create permission being revoked between opening the
	// chat and confirming (spec Edge Cases): downgrade the owner's own
	// app_members role to viewer.
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.app_members SET role = 'viewer' WHERE backend_app_id = $1 AND user_id = $2`, app.ID, user.ID); err != nil {
		t.Fatalf("downgrade role: %v", err)
	}

	req := confirmRequestFor(user, session.ID, "")
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	finalSession, _, err := loadOwnedBuildChatSession(ctx, pool, session.ID, user.ID)
	if err != nil {
		t.Fatalf("loadOwnedBuildChatSession: %v", err)
	}
	if finalSession.Status != "in_progress" {
		t.Fatalf("expected session to remain in_progress after a forbidden retry, got %q", finalSession.Status)
	}
}

// AIBC-11: BuildChatConfirm's own owner-scoped lookup (loadOwnedBuildChatSession)
// rejects a session ID that belongs to a different user — an IDOR-shaped
// check distinct from the store-level scoping already covered in
// ai_build_sessions_store_test.go.
func TestBuildChatConfirm_AnotherUsersSessionReturnsNotFoundNoMutation(t *testing.T) {
	pool, h, owner := aiBuildChatHandlerTestPool(t)
	ctx := context.Background()

	otherUser, err := CreateUser(ctx, pool, fmt.Sprintf("build-chat-other-%d@example.com", time.Now().UnixNano()), "Other User", "hash", "admin")
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}

	session, _, err := GetOrCreateInProgressSession(ctx, pool, owner.ID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	plan := &ai.AppPlan{
		Name:   "not-yours-app",
		Tables: []ai.PlanTable{{Name: "tickets", Columns: []ai.PlanColumn{{Name: "title", Type: "text"}}}},
	}
	persistProposedPlan(t, pool, session.ID, plan)

	req := confirmRequestFor(otherUser, session.ID, "")
	w := httptest.NewRecorder()
	h.BuildChatConfirm(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 confirming another user's session, got %d: %s", w.Code, w.Body.String())
	}

	apps, err := ListAppsForUser(ctx, pool, owner)
	if err != nil {
		t.Fatalf("ListAppsForUser: %v", err)
	}
	for _, a := range apps {
		if a.Name == "not-yours-app" {
			t.Fatalf("expected no app created from another user's confirm attempt, found %+v", a)
		}
	}

	ownerSession, _, err := loadOwnedBuildChatSession(ctx, pool, session.ID, owner.ID)
	if err != nil {
		t.Fatalf("loadOwnedBuildChatSession: %v", err)
	}
	if ownerSession.Status != "in_progress" {
		t.Fatalf("expected the owner's session to remain in_progress after another user's confirm attempt, got %q", ownerSession.Status)
	}
}
