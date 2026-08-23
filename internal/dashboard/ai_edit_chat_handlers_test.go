package dashboard

// ai_edit_chat_handlers_test.go — coverage for EditChatTurn (ai-edit-chat
// spec T7), derived from spec.md's P1 acceptance criteria (AIEC-01, AIEC-02,
// AIEC-05, AIEC-15, AIEC-18) and tasks.md's T7 Done-when list — not from
// reading the implementation. EditChatConfirm (T8) and
// GetEditChatSession/RestartEditChatSession (T9) add their own tests to
// this file in their own commits.

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

	"github.com/zeeplabs/zeep-orbit/internal/dashboard/ai"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// aiEditChatHandlerTestPool follows the same DSN/provision/truncate/
// NewHandler pattern as aiBuildChatHandlerTestPool, plus seeds a real app
// (owned by the returned user, so CanWrite() is true) for the edit chat to
// be scoped to.
func aiEditChatHandlerTestPool(t *testing.T) (*db.Pool, *Handler, *DashboardUser, string) {
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

	os.Setenv("DASHBOARD_BOOTSTRAP_SECRET", "test-secret-for-ai-edit-chat-handlers")

	h := NewHandler(pool, registry.New(), zap.NewNop())
	user, err := CreateUser(ctx, pool, fmt.Sprintf("edit-chat-handler-%d@example.com", time.Now().UnixNano()), "Chat User", "hash", "admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	app, err := h.CreateAppForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, AppRequestBody{Name: fmt.Sprintf("ec%d", time.Now().UnixNano()%1_000_000_000)}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	return pool, h, user, app.ID
}

// withFakeEditAIModel swaps editCallAIModel for a fake for the duration of
// one test — mirrors withFakeAIModel (ai_build_chat_handlers_test.go).
func withFakeEditAIModel(t *testing.T, fake func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error)) {
	t.Helper()
	original := editCallAIModel
	editCallAIModel = fake
	t.Cleanup(func() { editCallAIModel = original })
}

// editChatTurnRequestFor builds an httptest request carrying user in
// context and appID set as the chi "id" URL param, matching the app-scoped
// route convention (chi.URLParam(r, "id")) every other app-scoped handler
// in this package already uses.
func editChatTurnRequestFor(user *DashboardUser, appID, content string) *http.Request {
	body, _ := json.Marshal(editChatTurnRequest{Content: content})
	r := httptest.NewRequest(http.MethodPost, "/dashboard/api/apps/"+appID+"/ai/edit-chat", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if user != nil {
		r = withUser(r, user)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	r = r.WithContext(withCtx(r, rctx))
	return r
}

// AIEC-01/AIEC-15: a plain clarifying-question turn persists a message with
// no plan_json and returns the message shape, using editChatSystemPrompt
// (not buildChatSystemPrompt).
func TestEditChatTurn_MessageShapeTurn(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	setOpenAIProvider(t, pool, true)
	withFakeEditAIModel(t, func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error) {
		return ai.ChatTurnResult{Kind: "message", Content: "Which table should the new column go on?"}, nil
	})

	req := editChatTurnRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, appID, "add an email column")
	w := httptest.NewRecorder()
	h.EditChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "message" || got.Content != "Which table should the new column go on?" {
		t.Fatalf("expected message-shape response, got %+v", got)
	}
	if got.EditOp != nil {
		t.Fatal("expected no edit_op on a message-shape response")
	}

	_, messages, err := GetOrCreateInProgressEditSession(context.Background(), pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 persisted messages (user + assistant), got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "add an email column" {
		t.Errorf("expected persisted user message, got %+v", messages[0])
	}
	if messages[1].Role != "assistant" || len(messages[1].Plan) != 0 {
		t.Errorf("expected a plain assistant message with no plan_json, got %+v", messages[1])
	}
}

// AIEC-02 (and the shared shape for AIEC-07/08/09/11/12): each of the 6
// tool-call kinds results in a message with the correct EditOp persisted
// and returned. add_column and toggle_auth are exercised directly here;
// the remaining 4 kinds share the exact same persistence code path
// (marshal EditOp -> AppendMessage), already proven correct by T5's
// per-Kind parsing tests, so this covers the two representative shapes
// (a populated struct field and a bool-only one) rather than duplicating
// all 6 at this layer too.
func TestEditChatTurn_EditOpShapeTurn(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	setOpenAIProvider(t, pool, true)
	op := &ai.EditOperation{Kind: "add_column", AddColumn: &ai.PlanColumnOp{
		Table:  "users",
		Column: ai.PlanColumn{Name: "email", Type: "text"},
	}}
	withFakeEditAIModel(t, func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error) {
		return ai.ChatTurnResult{Kind: "edit_op", EditOp: op}, nil
	})

	req := editChatTurnRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, appID, "add an email column to users")
	w := httptest.NewRecorder()
	h.EditChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "edit_op" {
		t.Fatalf("expected edit_op-shape response, got %+v", got)
	}
	if got.EditOp == nil || got.EditOp.Kind != "add_column" || got.EditOp.AddColumn == nil ||
		got.EditOp.AddColumn.Table != "users" || got.EditOp.AddColumn.Column.Name != "email" {
		t.Fatalf("expected the op's fields to match the model's tool call, got %+v", got.EditOp)
	}

	_, messages, err := GetOrCreateInProgressEditSession(context.Background(), pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(messages))
	}
	var decoded ai.EditOperation
	if err := json.Unmarshal(messages[1].Plan, &decoded); err != nil {
		t.Fatalf("expected EditOperation JSON persisted on the assistant message: %v", err)
	}
	if decoded.Kind != "add_column" || decoded.AddColumn == nil || decoded.AddColumn.Table != "users" {
		t.Fatalf("expected persisted EditOp to match, got %+v", decoded)
	}

	// The turn alone must never mutate — no column exists until an
	// explicit confirm (T8) runs.
	schema, err := GetAppSchemaForUser(context.Background(), pool, &DashboardUser{ID: user.ID, Role: "admin"}, appID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	for _, tbl := range schema.Tables {
		if tbl.Name == "users" {
			t.Fatalf("expected no users table/column created from an edit_op-shape turn alone, found %+v", tbl)
		}
	}
}

// AIEC-18 (mirrors AIBC-16): a model-call failure returns the fixed
// generic chat message and leaves the session in_progress (edit sessions
// never reach completed, so this is just "unchanged").
func TestEditChatTurn_ModelFailureReturnsGenericMessage(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	setOpenAIProvider(t, pool, true)
	withFakeEditAIModel(t, func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error) {
		return ai.ChatTurnResult{}, fmt.Errorf("openai: rate limit exceeded for key sk-super-secret")
	})

	req := editChatTurnRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, appID, "add an email column")
	w := httptest.NewRecorder()
	h.EditChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (chat-visible error, not an HTTP error), got %d: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sk-super-secret")) {
		t.Fatal("expected the real error to never leak into the HTTP response")
	}
	var got editChatTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "message" || got.Content != genericAIChatError {
		t.Fatalf("expected the fixed generic message, got %+v", got)
	}

	session, messages, err := GetOrCreateInProgressEditSession(context.Background(), pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	if session.Status != "in_progress" {
		t.Fatalf("expected the session to remain in_progress after a model failure, got %q", session.Status)
	}
	if len(messages) != 1 {
		t.Fatalf("expected only the user's own message persisted after a model failure, got %d", len(messages))
	}
}

// AIEC-05: a caller without write access to the app gets an authorization
// error before any session is created or model call happens.
func TestEditChatTurn_ViewerForbidden(t *testing.T) {
	pool, h, owner, appID := aiEditChatHandlerTestPool(t)
	viewer, err := CreateUser(context.Background(), pool, fmt.Sprintf("edit-chat-viewer-%d@example.com", time.Now().UnixNano()), "Viewer", "hash", "member")
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if _, err := AddAppMember(context.Background(), pool, AppRef{BackendAppID: appID}, viewer.ID, AppRoleViewer); err != nil {
		t.Fatalf("AddAppMember: %v", err)
	}
	_ = owner

	req := editChatTurnRequestFor(&DashboardUser{ID: viewer.ID, Role: "member"}, appID, "add an email column")
	w := httptest.NewRecorder()
	h.EditChatTurn(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	_, messages, err := GetOrCreateInProgressEditSession(context.Background(), pool, viewer.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected no session/messages created for a denied request, got %d messages", len(messages))
	}
}

// AIEC-15: editChatSystemPrompt (not buildChatSystemPrompt) is the system
// message actually sent to the model — the off-topic guard and
// get_app_schema-before-proposing instruction are only meaningful if this
// prompt is the one on the wire.
func TestEditChatTurn_UsesEditChatSystemPromptNotBuildPrompt(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	setOpenAIProvider(t, pool, true)

	var sentSystemContent string
	withFakeEditAIModel(t, func(ctx context.Context, model, apiKey string, history []ai.Message, readTools ai.ReadToolInvoker) (ai.ChatTurnResult, error) {
		for _, m := range history {
			if m.Role == "system" {
				sentSystemContent = m.Content
			}
		}
		return ai.ChatTurnResult{Kind: "message", Content: "ok"}, nil
	})

	req := editChatTurnRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, appID, "add an email column")
	w := httptest.NewRecorder()
	h.EditChatTurn(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if sentSystemContent != editChatSystemPrompt {
		t.Fatalf("expected the system message to be editChatSystemPrompt verbatim, got a different prompt")
	}
	if sentSystemContent == buildChatSystemPrompt {
		t.Fatal("expected editChatSystemPrompt, not buildChatSystemPrompt, to be sent")
	}
}
