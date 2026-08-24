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
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/config"
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
// no plan_json and returns the message shape, using editChatSystemPromptFor
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

// AIEC-14 (mirrors AIBC-16): a model-call failure returns the fixed
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

// AIEC-15: editChatSystemPromptFor (not buildChatSystemPrompt) is the system
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
	app, _, err := GetApp(context.Background(), pool, appID, &DashboardUser{ID: user.ID, Role: "admin"})
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if sentSystemContent != editChatSystemPromptFor(app.Name) {
		t.Fatalf("expected the system message to be editChatSystemPromptFor(%q) verbatim, got a different prompt", app.Name)
	}
	if sentSystemContent == buildChatSystemPrompt {
		t.Fatal("expected editChatSystemPromptFor, not buildChatSystemPrompt, to be sent")
	}
}

// confirmEditChatRequestFor builds an httptest request carrying user in
// context and sessionID as the chi "session_id" URL param — mirrors
// confirmRequestFor (ai_build_chat_handlers_test.go) for the edit-chat
// confirm endpoint, which (unlike BuildChatConfirm) takes no request body
// at all.
func confirmEditChatRequestFor(user *DashboardUser, sessionID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/dashboard/api/apps/x/ai/edit-chat/"+sessionID+"/confirm", nil)
	if user != nil {
		r = withUser(r, user)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("session_id", sessionID)
	r = r.WithContext(withCtx(r, rctx))
	return r
}

// persistProposedEditOp appends an assistant message carrying op as its
// plan_json — the only way EditChatConfirm ever learns of a pending
// operation.
func persistProposedEditOp(t *testing.T, pool *db.Pool, sessionID string, op *ai.EditOperation) {
	t.Helper()
	opJSON, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal op: %v", err)
	}
	if err := AppendMessage(context.Background(), pool, sessionID, "assistant", "", opJSON); err != nil {
		t.Fatalf("persistProposedEditOp: %v", err)
	}
}

// AIEC-02/AIEC-06: confirming a pending add_column op calls
// AddTableColumnForUser, persists the applied marker, and audits with
// origin "ai_chat".
func TestEditChatConfirm_AddColumn(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_column", AddColumn: &ai.PlanColumnOp{
		Table:  "users",
		Column: ai.PlanColumn{Name: "email", Type: "text"},
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Applied || got.Kind != "add_column" || got.Table == nil {
		t.Fatalf("expected an applied add_column response with the updated table, got %+v", got)
	}
	var foundEmail bool
	for _, c := range got.Table.Columns {
		if c.Name == "email" && c.Type == "text" {
			foundEmail = true
		}
	}
	if !foundEmail {
		t.Fatalf("expected the email column in the returned table, got %+v", got.Table.Columns)
	}

	finalSession, _, err := loadOwnedEditChatSession(ctx, pool, session.ID, user.ID)
	if err != nil {
		t.Fatalf("loadOwnedEditChatSession: %v", err)
	}
	if finalSession.Status != "in_progress" {
		t.Fatalf("expected the session to remain in_progress, got %q", finalSession.Status)
	}

	var origin string
	if err := pool.QueryRow(ctx,
		`SELECT ip_address FROM zeep_system.audit_log WHERE resource_id = $1 AND action = 'app.table_column.create'`, got.Table.ID,
	).Scan(&origin); err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if origin != "ai_chat" {
		t.Fatalf("expected audit origin %q, got %q", "ai_chat", origin)
	}
}

// AIEC-07: confirming a pending add_index op calls AddTableIndexForUser.
func TestEditChatConfirm_AddIndex(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "email", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_index", AddIndex: &ai.PlanIndexOp{
		Table: "users", Name: "users_email_idx", Columns: []string{"email"}, Unique: true,
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var foundIdx bool
	for _, idx := range got.Table.Indexes {
		if idx.Name == "users_email_idx" && idx.Unique {
			foundIdx = true
		}
	}
	if !foundIdx {
		t.Fatalf("expected the users_email_idx index in the returned table, got %+v", got.Table.Indexes)
	}
}

// AIEC-08: confirming a pending add_table op calls CreateAppTableForUser.
func TestEditChatConfirm_AddTable(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_table", AddTable: &ai.PlanTable{
		Name: "notes", Columns: []ai.PlanColumn{{Name: "body", Type: "text"}},
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Table == nil || got.Table.Name != "notes" {
		t.Fatalf("expected the created notes table, got %+v", got.Table)
	}
}

// AIEC-09: confirming a pending add_reference op calls AddTableColumnForUser
// with References populated.
func TestEditChatConfirm_AddReference(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser (users): %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "tickets",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser (tickets): %v", err)
	}

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_reference", AddReference: &ai.PlanReferenceOp{
		Table:     "tickets",
		Column:    ai.PlanColumn{Name: "assignee_id", Type: "uuid"},
		RefTable:  "users",
		RefColumn: "id",
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var foundRef bool
	for _, c := range got.Table.Columns {
		if c.Name == "assignee_id" && c.References != nil && c.References.Table == "users" {
			foundRef = true
		}
	}
	if !foundRef {
		t.Fatalf("expected assignee_id with References.Table=users, got %+v", got.Table.Columns)
	}
}

// AIEC-11: confirming a pending set_rls_mode op calls UpdateTableRLSModeForUser.
func TestEditChatConfirm_SetRLSMode(t *testing.T) {
	pool, h, user, _ := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	// "enabled" RLS requires email auth on the app (validateTableInput's
	// RLSxauthEmail invariant) — the pool's default app has no auth, so
	// this test creates its own app with auth enabled.
	app, err := h.CreateAppForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, AppRequestBody{
		Name:             fmt.Sprintf("ecrls%d", time.Now().UnixNano()%1_000_000_000),
		AuthEmailEnabled: true,
	}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	appID := app.ID
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "tickets",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "set_rls_mode", SetRLSMode: &ai.PlanRLSOp{Table: "tickets", Mode: "enabled"}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Table == nil || got.Table.RLS != "enabled" {
		t.Fatalf("expected rls=enabled, got %+v", got.Table)
	}
}

// AIEC-12: confirming a pending toggle_auth op calls UpdateAppForUser.
func TestEditChatConfirm_ToggleAuth(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "toggle_auth", ToggleAuth: &ai.PlanAuthOp{EmailEnabled: true}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatConfirmResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.App == nil || !got.App.AuthEmailEnabled {
		t.Fatalf("expected auth_email_enabled=true, got %+v", got.App)
	}
}

// AIEC-04: a handler validation error (duplicate column name) surfaces its
// specific message verbatim, the session stays in_progress, and the app is
// unmodified.
func TestEditChatConfirm_DuplicateColumnSurfacesSpecificError(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "email", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_column", AddColumn: &ai.PlanColumnOp{
		Table:  "users",
		Column: ai.PlanColumn{Name: "email", Type: "text"}, // already exists
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(got["error"], "already exists") {
		t.Fatalf("expected the specific duplicate-column error, got %+v", got)
	}

	finalSession, _, err := loadOwnedEditChatSession(ctx, pool, session.ID, user.ID)
	if err != nil {
		t.Fatalf("loadOwnedEditChatSession: %v", err)
	}
	if finalSession.Status != "in_progress" {
		t.Fatalf("expected the session to remain in_progress, got %q", finalSession.Status)
	}

	schema, err := GetAppSchemaForUser(ctx, pool, &DashboardUser{ID: user.ID, Role: "admin"}, appID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	for _, tbl := range schema.Tables {
		if tbl.Name != "users" {
			continue
		}
		count := 0
		for _, c := range tbl.Columns {
			if c.Name == "email" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 email column (app unmodified), got %d", count)
		}
	}
}

// AIEC-18: a proposed operation referencing a table that doesn't exist in
// the app's current schema is rejected with the underlying handler's own
// not-found error, session unchanged, app unmodified — the chat never
// silently invents or guesses the missing table.
func TestEditChatConfirm_NonexistentTableSurfacesNotFound(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_column", AddColumn: &ai.PlanColumnOp{
		Table:  "does_not_exist",
		Column: ai.PlanColumn{Name: "email", Type: "text"},
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	finalSession, _, err := loadOwnedEditChatSession(ctx, pool, session.ID, user.ID)
	if err != nil {
		t.Fatalf("loadOwnedEditChatSession: %v", err)
	}
	if finalSession.Status != "in_progress" {
		t.Fatalf("expected the session to remain in_progress, got %q", finalSession.Status)
	}
}

// Discrimination-sensor coverage: the non-in_progress session guard in
// EditChatConfirm must actually reject a confirm call against a session
// that's already abandoned (e.g. via Recomeçar, or a stale client retrying
// an old session) rather than silently applying the stale pending op.
func TestEditChatConfirm_AbandonedSessionRejected(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_column", AddColumn: &ai.PlanColumnOp{
		Table:  "users",
		Column: ai.PlanColumn{Name: "email", Type: "text"},
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	if _, err := AbandonAndRestartEditSession(ctx, pool, user.ID, appID); err != nil {
		t.Fatalf("AbandonAndRestartEditSession: %v", err)
	}

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a confirm against an abandoned session, got %d: %s", w.Code, w.Body.String())
	}

	schema, err := GetAppSchemaForUser(ctx, pool, &DashboardUser{ID: user.ID, Role: "admin"}, appID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	for _, tbl := range schema.Tables {
		if tbl.Name != "users" {
			continue
		}
		for _, c := range tbl.Columns {
			if c.Name == "email" {
				t.Fatalf("expected the pending op to NOT apply against an abandoned session, but found email column")
			}
		}
	}
}

// AIEC-05: a viewer (CanWrite()==false) is denied before any handler runs,
// for the add_column Kind (representative of every Kind, since RBAC is
// enforced identically by every dispatched *ForUser handler).
func TestEditChatConfirm_ViewerForbidden(t *testing.T) {
	pool, h, owner, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: owner.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	viewer, err := CreateUser(ctx, pool, fmt.Sprintf("edit-chat-confirm-viewer-%d@example.com", time.Now().UnixNano()), "Viewer", "hash", "member")
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if _, err := AddAppMember(ctx, pool, AppRef{BackendAppID: appID}, viewer.ID, AppRoleViewer); err != nil {
		t.Fatalf("AddAppMember: %v", err)
	}

	// The viewer has their own in_progress edit session for this app (a
	// legitimate scenario if they'd been granted write access earlier and
	// it was since revoked) with a pending op persisted on it.
	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, viewer.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_column", AddColumn: &ai.PlanColumnOp{
		Table:  "users",
		Column: ai.PlanColumn{Name: "email", Type: "text"},
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: viewer.ID, Role: "member"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	schema, err := GetAppSchemaForUser(ctx, pool, &DashboardUser{ID: owner.ID, Role: "admin"}, appID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	for _, tbl := range schema.Tables {
		if tbl.Name != "users" {
			continue
		}
		for _, c := range tbl.Columns {
			if c.Name == "email" {
				t.Fatal("expected no email column created for a forbidden confirm")
			}
		}
	}
}

// IDOR guard: a session belonging to another user returns not-found, with
// no mutation — mirrors TestBuildChatConfirm_AnotherUsersSessionReturnsNotFoundNoMutation.
func TestEditChatConfirm_AnotherUsersSessionReturnsNotFoundNoMutation(t *testing.T) {
	pool, h, owner, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: owner.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	otherUser, err := CreateUser(ctx, pool, fmt.Sprintf("edit-chat-other-%d@example.com", time.Now().UnixNano()), "Other User", "hash", "admin")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, owner.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_column", AddColumn: &ai.PlanColumnOp{
		Table:  "users",
		Column: ai.PlanColumn{Name: "email", Type: "text"},
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req := confirmEditChatRequestFor(&DashboardUser{ID: otherUser.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	schema, err := GetAppSchemaForUser(ctx, pool, &DashboardUser{ID: owner.ID, Role: "admin"}, appID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	for _, tbl := range schema.Tables {
		if tbl.Name != "users" {
			continue
		}
		for _, c := range tbl.Columns {
			if c.Name == "email" {
				t.Fatal("expected no email column created by another user's confirm attempt")
			}
		}
	}
}

// AIEC-16: a repeat confirm call after a successful apply is a no-op, not a
// duplicate mutation — asserted by confirming the same add_column op twice
// and checking the second call neither errors nor creates a second column.
func TestEditChatConfirm_DoubleConfirmIsNoOp(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()
	if _, err := h.CreateAppTableForUser(ctx, &DashboardUser{ID: user.ID, Role: "admin"}, appID, TableRequestBody{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	op := &ai.EditOperation{Kind: "add_column", AddColumn: &ai.PlanColumnOp{
		Table:  "users",
		Column: ai.PlanColumn{Name: "email", Type: "text"},
	}}
	persistProposedEditOp(t, pool, session.ID, op)

	req1 := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w1 := httptest.NewRecorder()
	h.EditChatConfirm(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 on first confirm, got %d: %s", w1.Code, w1.Body.String())
	}

	req2 := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w2 := httptest.NewRecorder()
	h.EditChatConfirm(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 on second (duplicate) confirm, got %d: %s", w2.Code, w2.Body.String())
	}
	var got2 editChatConfirmResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &got2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got2.Applied {
		t.Fatalf("expected the second confirm to report applied=true (no-op), got %+v", got2)
	}

	schema, err := GetAppSchemaForUser(ctx, pool, &DashboardUser{ID: user.ID, Role: "admin"}, appID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	for _, tbl := range schema.Tables {
		if tbl.Name != "users" {
			continue
		}
		count := 0
		for _, c := range tbl.Columns {
			if c.Name == "email" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 email column after two confirms (no duplicate mutation), got %d", count)
		}
	}
}

// editChatSessionRequestFor builds an httptest request carrying user in
// context and appID set as the chi "id" URL param — for GetEditChatSession/
// RestartEditChatSession, which take no request body.
func editChatSessionRequestFor(user *DashboardUser, appID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/dashboard/api/apps/"+appID+"/ai/edit-chat", nil)
	if user != nil {
		r = withUser(r, user)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	r = r.WithContext(withCtx(r, rctx))
	return r
}

// AIEC-01 (reload half): reopening the drawer for an app with an
// in_progress edit session reloads its messages instead of creating a new
// one.
func TestGetEditChatSession_ReloadsExistingHistory(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)

	session, err := createEditSessionWithMessage(t, pool, user.ID, appID, "add an email column")
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	req := editChatSessionRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, appID)
	w := httptest.NewRecorder()
	h.GetEditChatSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Session == nil || got.Session.ID != session.ID {
		t.Fatalf("expected the existing in_progress session to be reloaded, got %+v", got.Session)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "add an email column" {
		t.Fatalf("expected the prior message reloaded, got %+v", got.Messages)
	}
}

// AIEC-01 (create half) + AIEC-05: a fresh app with no in_progress edit
// session yet gets a newly created one; a viewer is forbidden instead.
func TestGetEditChatSession_CreatesFreshSessionAndEnforcesWriteAccess(t *testing.T) {
	pool, h, owner, appID := aiEditChatHandlerTestPool(t)

	req := editChatSessionRequestFor(&DashboardUser{ID: owner.ID, Role: "admin"}, appID)
	w := httptest.NewRecorder()
	h.GetEditChatSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Session == nil || got.Session.Status != "in_progress" || len(got.Messages) != 0 {
		t.Fatalf("expected a fresh empty in_progress session, got %+v / %d messages", got.Session, len(got.Messages))
	}

	viewer, err := CreateUser(context.Background(), pool, fmt.Sprintf("edit-chat-getsession-viewer-%d@example.com", time.Now().UnixNano()), "Viewer", "hash", "member")
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if _, err := AddAppMember(context.Background(), pool, AppRef{BackendAppID: appID}, viewer.ID, AppRoleViewer); err != nil {
		t.Fatalf("AddAppMember: %v", err)
	}
	viewerReq := editChatSessionRequestFor(&DashboardUser{ID: viewer.ID, Role: "member"}, appID)
	viewerW := httptest.NewRecorder()
	h.GetEditChatSession(viewerW, viewerReq)
	if viewerW.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a viewer, got %d: %s", viewerW.Code, viewerW.Body.String())
	}
}

// Restart (spec Edge Cases, "Recomeçar"): abandons the current in_progress
// edit session (preserving its messages) and returns a fresh empty one,
// without requiring any pending operation to be resolved first.
func TestRestartEditChatSession_AbandonsAndCreatesFresh(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)

	session, err := createEditSessionWithMessage(t, pool, user.ID, appID, "add an email column")
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	// A pending (unconfirmed) op left on the session — restart must not
	// require it to be resolved first.
	persistProposedEditOp(t, pool, session.ID, &ai.EditOperation{
		Kind: "add_column",
		AddColumn: &ai.PlanColumnOp{
			Table:  "users",
			Column: ai.PlanColumn{Name: "email", Type: "text"},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/dashboard/api/apps/"+appID+"/ai/edit-chat/restart", nil)
	req = withUser(req, &DashboardUser{ID: user.ID, Role: "admin"})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	req = req.WithContext(withCtx(req, rctx))
	w := httptest.NewRecorder()
	h.RestartEditChatSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got editChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Session == nil || got.Session.ID == session.ID {
		t.Fatalf("expected a new session distinct from the original, got %+v", got.Session)
	}
	if len(got.Messages) != 0 {
		t.Fatalf("expected the fresh session to start with no messages, got %d", len(got.Messages))
	}

	var oldStatus string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM zeep_system.ai_build_sessions WHERE id = $1`, session.ID,
	).Scan(&oldStatus); err != nil {
		t.Fatalf("query old session status: %v", err)
	}
	if oldStatus != "abandoned" {
		t.Fatalf("expected the original session abandoned, got %q", oldStatus)
	}
}

// createEditSessionWithMessage seeds an in_progress edit session for
// (userID, appID) with one persisted user message — shared setup for the
// GetEditChatSession/RestartEditChatSession tests above.
func createEditSessionWithMessage(t *testing.T, pool *db.Pool, userID, appID, content string) (*AIBuildSession, error) {
	t.Helper()
	session, _, err := GetOrCreateInProgressEditSession(context.Background(), pool, userID, appID)
	if err != nil {
		return nil, err
	}
	if err := AppendMessage(context.Background(), pool, session.ID, "user", content, nil); err != nil {
		return nil, err
	}
	return session, nil
}

// No pending operation at all (fresh session, never had a turn) is
// rejected with a 400, matching BuildChatConfirm's equivalent guard.
func TestEditChatConfirm_NoPendingOperationRejected(t *testing.T) {
	pool, h, user, appID := aiEditChatHandlerTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressEditSession(ctx, pool, user.ID, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}

	req := confirmEditChatRequestFor(&DashboardUser{ID: user.ID, Role: "admin"}, session.ID)
	w := httptest.NewRecorder()
	h.EditChatConfirm(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestEditChatSystemPromptFor_RoutesForeignKeyRequests covers
// column-foreign-key spec CFK-16/T12: the rendered prompt names both new
// tools and no longer instructs the model to decline a foreign-key request
// on an existing column.
func TestEditChatSystemPromptFor_RoutesForeignKeyRequests(t *testing.T) {
	prompt := editChatSystemPromptFor("demo-app")

	if !strings.Contains(prompt, "propose_add_foreign_key") {
		t.Fatalf("expected the prompt to mention propose_add_foreign_key, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "propose_remove_foreign_key") {
		t.Fatalf("expected the prompt to mention propose_remove_foreign_key, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "decline and explain that this chat can't recreate an existing column") {
		t.Fatalf("expected the old decline-a-foreign-key-request sentence to be gone, got:\n%s", prompt)
	}
}
