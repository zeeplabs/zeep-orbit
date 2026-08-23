package dashboard

// ai_build_sessions_store_test.go — coverage for session lifecycle
// (resume/create/restart/complete) and owner scoping. Derived from
// spec.md's P2 acceptance criteria (AIBC-07 through AIBC-11) and
// tasks.md's T6 Done-when list — not from reading the implementation.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func aiBuildSessionsTestPool(t *testing.T) (*db.Pool, string, string) {
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
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.ai_build_messages`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.ai_build_sessions`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.dashboard_users CASCADE`)
	}
	truncate()
	t.Cleanup(truncate)

	userA, err := CreateUser(ctx, pool, fmt.Sprintf("build-chat-a-%d@example.com", time.Now().UnixNano()), "A", "hash", "member")
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	userB, err := CreateUser(ctx, pool, fmt.Sprintf("build-chat-b-%d@example.com", time.Now().UnixNano()), "B", "hash", "member")
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}

	return pool, userA.ID, userB.ID
}

// AIBC-08: with no in_progress session, GetOrCreateInProgressSession
// creates a new one scoped to the user, with an empty message history.
func TestGetOrCreateInProgressSession_CreatesWhenNoneExists(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	ctx := context.Background()

	session, messages, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	if session.OwnerUserID != userA {
		t.Errorf("expected owner_user_id %q, got %q", userA, session.OwnerUserID)
	}
	if session.Status != "in_progress" {
		t.Errorf("expected status in_progress, got %q", session.Status)
	}
	if len(messages) != 0 {
		t.Errorf("expected empty message history for a freshly created session, got %d", len(messages))
	}
}

// AIBC-07: an existing in_progress session is resumed (same ID) along with
// its full message history, instead of creating a new session.
func TestGetOrCreateInProgressSession_ResumesExistingWithHistory(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	ctx := context.Background()

	first, _, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession (first): %v", err)
	}
	if err := AppendMessage(ctx, pool, first.ID, "user", "I want a ticketing app", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := AppendMessage(ctx, pool, first.ID, "assistant", "Do you need authentication?", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	second, messages, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession (second): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same session to be resumed, got a different ID (%q vs %q)", second.ID, first.ID)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages in history, got %d", len(messages))
	}
	if messages[0].Content != "I want a ticketing app" || messages[1].Content != "Do you need authentication?" {
		t.Errorf("expected messages in insertion order, got %+v", messages)
	}
}

// AIBC-09: Restart abandons the current in_progress session (old messages
// remain in storage, status flips to abandoned) and creates a fresh
// in_progress session.
func TestAbandonAndRestartSession_PreservesHistoryAndCreatesFresh(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	ctx := context.Background()

	original, _, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	if err := AppendMessage(ctx, pool, original.ID, "user", "some message", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	fresh, err := AbandonAndRestartSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("AbandonAndRestartSession: %v", err)
	}
	if fresh.ID == original.ID {
		t.Fatal("expected a new session ID after restart")
	}
	if fresh.Status != "in_progress" {
		t.Errorf("expected fresh session status in_progress, got %q", fresh.Status)
	}

	var oldStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM zeep_system.ai_build_sessions WHERE id = $1`, original.ID,
	).Scan(&oldStatus); err != nil {
		t.Fatalf("query old session status: %v", err)
	}
	if oldStatus != "abandoned" {
		t.Errorf("expected old session status abandoned, got %q", oldStatus)
	}

	var oldMessageCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM zeep_system.ai_build_messages WHERE session_id = $1`, original.ID,
	).Scan(&oldMessageCount); err != nil {
		t.Fatalf("count old messages: %v", err)
	}
	if oldMessageCount != 1 {
		t.Errorf("expected the abandoned session's message to remain in storage, got count %d", oldMessageCount)
	}
}

// AIBC-11: sessions and messages are scoped to owner_user_id — user B's
// resume call must never see user A's in_progress session or history.
func TestGetOrCreateInProgressSession_ScopedToOwner(t *testing.T) {
	pool, userA, userB := aiBuildSessionsTestPool(t)
	ctx := context.Background()

	sessionA, _, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession (A): %v", err)
	}
	if err := AppendMessage(ctx, pool, sessionA.ID, "user", "user A's private message", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	sessionB, messagesB, err := GetOrCreateInProgressSession(ctx, pool, userB)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession (B): %v", err)
	}
	if sessionB.ID == sessionA.ID {
		t.Fatal("expected user B to get a distinct session, not user A's")
	}
	if sessionB.OwnerUserID != userB {
		t.Errorf("expected session B owner %q, got %q", userB, sessionB.OwnerUserID)
	}
	if len(messagesB) != 0 {
		t.Fatalf("expected user B's session to have no messages (never user A's), got %d", len(messagesB))
	}
}

// AIBC-10: CompleteSession sets status=completed and created_app_id in one
// call, and SetSessionCreatedApp can be called independently beforehand
// (the partial-failure requirement, AIBC-22) without first calling
// CompleteSession.
func TestCompleteSession_And_SetSessionCreatedApp_AreIndependent(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}

	const partialAppID = "11111111-1111-1111-1111-111111111111"
	if err := SetSessionCreatedApp(ctx, pool, session.ID, partialAppID); err != nil {
		t.Fatalf("SetSessionCreatedApp: %v", err)
	}

	var statusAfterSet string
	var appIDAfterSet *string
	if err := pool.QueryRow(ctx,
		`SELECT status, created_app_id FROM zeep_system.ai_build_sessions WHERE id = $1`, session.ID,
	).Scan(&statusAfterSet, &appIDAfterSet); err != nil {
		t.Fatalf("query after SetSessionCreatedApp: %v", err)
	}
	if statusAfterSet != "in_progress" {
		t.Errorf("expected status to remain in_progress after SetSessionCreatedApp alone, got %q", statusAfterSet)
	}
	if appIDAfterSet == nil || *appIDAfterSet != partialAppID {
		t.Errorf("expected created_app_id %q set independently of CompleteSession, got %v", partialAppID, appIDAfterSet)
	}

	const finalAppID = "22222222-2222-2222-2222-222222222222"
	if err := CompleteSession(ctx, pool, session.ID, finalAppID); err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}

	var statusAfterComplete string
	var appIDAfterComplete *string
	if err := pool.QueryRow(ctx,
		`SELECT status, created_app_id FROM zeep_system.ai_build_sessions WHERE id = $1`, session.ID,
	).Scan(&statusAfterComplete, &appIDAfterComplete); err != nil {
		t.Fatalf("query after CompleteSession: %v", err)
	}
	if statusAfterComplete != "completed" {
		t.Errorf("expected status completed, got %q", statusAfterComplete)
	}
	if appIDAfterComplete == nil || *appIDAfterComplete != finalAppID {
		t.Errorf("expected created_app_id %q, got %v", finalAppID, appIDAfterComplete)
	}
}

// aiEditSessionTestApp creates a real app owned by ownerUserID so
// GetOrCreateInProgressEditSession has a real target_app_id to satisfy the
// ai_build_sessions.target_app_id foreign key against zeep_system.apps.
func aiEditSessionTestApp(t *testing.T, pool *db.Pool, ownerUserID string) string {
	t.Helper()
	app, err := CreateApp(context.Background(), pool, fmt.Sprintf("edit-chat-app-%d", time.Now().UnixNano()), ownerUserID, false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	return app.ID
}

// AIEC-01 (create half): with no in_progress edit session for (user, app),
// GetOrCreateInProgressEditSession creates a new one with target_app_id set
// at creation and an empty message history.
func TestGetOrCreateInProgressEditSession_CreatesWhenNoneExists(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	appID := aiEditSessionTestApp(t, pool, userA)

	session, messages, err := GetOrCreateInProgressEditSession(context.Background(), pool, userA, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	if session.OwnerUserID != userA {
		t.Errorf("expected owner_user_id %q, got %q", userA, session.OwnerUserID)
	}
	if session.Status != "in_progress" {
		t.Errorf("expected status in_progress, got %q", session.Status)
	}
	if session.Mode != "edit" {
		t.Errorf("expected mode %q, got %q", "edit", session.Mode)
	}
	if session.TargetAppID == nil || *session.TargetAppID != appID {
		t.Errorf("expected target_app_id %q set at creation, got %v", appID, session.TargetAppID)
	}
	if len(messages) != 0 {
		t.Errorf("expected empty message history for a freshly created edit session, got %d", len(messages))
	}
}

// AIEC-01 (resume half): an existing in_progress edit session for the same
// (user, app) is resumed (same ID) along with its full message history.
func TestGetOrCreateInProgressEditSession_ResumesExistingWithHistory(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	appID := aiEditSessionTestApp(t, pool, userA)
	ctx := context.Background()

	first, _, err := GetOrCreateInProgressEditSession(ctx, pool, userA, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession (first): %v", err)
	}
	if err := AppendMessage(ctx, pool, first.ID, "user", "add an email column to users", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	second, messages, err := GetOrCreateInProgressEditSession(ctx, pool, userA, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession (second): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same edit session to be resumed, got a different ID (%q vs %q)", second.ID, first.ID)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message in history, got %d", len(messages))
	}
	if messages[0].Content != "add an email column to users" {
		t.Errorf("expected the prior message content, got %+v", messages)
	}
}

// AIEC-17: a create-mode session for the user and an edit-mode session for
// an app coexist independently — creating/resuming the edit session must
// never surface or interfere with the user's in_progress create session,
// and vice versa.
func TestGetOrCreateInProgressEditSession_CoexistsWithCreateSession(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	appID := aiEditSessionTestApp(t, pool, userA)
	ctx := context.Background()

	createSession, _, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}

	editSession, editMessages, err := GetOrCreateInProgressEditSession(ctx, pool, userA, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	if editSession.ID == createSession.ID {
		t.Fatal("expected the edit session to be a distinct row from the create session")
	}
	if editSession.Mode != "edit" || createSession.Mode != "create" {
		t.Errorf("expected modes edit/create, got %q/%q", editSession.Mode, createSession.Mode)
	}
	if len(editMessages) != 0 {
		t.Fatalf("expected the edit session to start with no messages, got %d", len(editMessages))
	}

	// The create session's own resume path must still return the create
	// session, unaffected by the edit session's existence.
	resumedCreate, _, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession (resume): %v", err)
	}
	if resumedCreate.ID != createSession.ID {
		t.Fatalf("expected the create session to still be resumed, got a different ID (%q vs %q)", resumedCreate.ID, createSession.ID)
	}
}

// Restart (spec Edge Cases, "Recomeçar"): AbandonAndRestartEditSession
// abandons the current in_progress edit session for (user, app) —
// preserving its messages — and creates a fresh one for the same pair,
// mirroring TestAbandonAndRestartSession_PreservesHistoryAndCreatesFresh
// for the edit-mode scoping.
func TestAbandonAndRestartEditSession_PreservesHistoryAndCreatesFresh(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	appID := aiEditSessionTestApp(t, pool, userA)
	ctx := context.Background()

	original, _, err := GetOrCreateInProgressEditSession(ctx, pool, userA, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession: %v", err)
	}
	if err := AppendMessage(ctx, pool, original.ID, "user", "add a column", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	fresh, err := AbandonAndRestartEditSession(ctx, pool, userA, appID)
	if err != nil {
		t.Fatalf("AbandonAndRestartEditSession: %v", err)
	}
	if fresh.ID == original.ID {
		t.Fatal("expected a new session ID after restart")
	}
	if fresh.Status != "in_progress" || fresh.Mode != "edit" {
		t.Errorf("expected a fresh in_progress edit session, got %+v", fresh)
	}
	if fresh.TargetAppID == nil || *fresh.TargetAppID != appID {
		t.Errorf("expected target_app_id %q on the fresh session, got %v", appID, fresh.TargetAppID)
	}

	var oldStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM zeep_system.ai_build_sessions WHERE id = $1`, original.ID,
	).Scan(&oldStatus); err != nil {
		t.Fatalf("query old session status: %v", err)
	}
	if oldStatus != "abandoned" {
		t.Errorf("expected old session status abandoned, got %q", oldStatus)
	}

	var oldMessageCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM zeep_system.ai_build_messages WHERE session_id = $1`, original.ID,
	).Scan(&oldMessageCount); err != nil {
		t.Fatalf("count old messages: %v", err)
	}
	if oldMessageCount != 1 {
		t.Errorf("expected the abandoned session's message to remain in storage, got count %d", oldMessageCount)
	}

	// The fresh session must be the one now resumed for (userA, appID).
	resumed, messages, err := GetOrCreateInProgressEditSession(ctx, pool, userA, appID)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressEditSession (resume): %v", err)
	}
	if resumed.ID != fresh.ID {
		t.Fatalf("expected the fresh session to be resumed, got a different ID (%q vs %q)", resumed.ID, fresh.ID)
	}
	if len(messages) != 0 {
		t.Fatalf("expected the fresh session to start with no messages, got %d", len(messages))
	}
}

// AIBC-14 (persistence half): AppendMessage stores the plan JSON on the
// assistant message that carries a propose_app_plan result.
func TestAppendMessage_PersistsPlanJSON(t *testing.T) {
	pool, userA, _ := aiBuildSessionsTestPool(t)
	ctx := context.Background()

	session, _, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}

	plan := json.RawMessage(`{"name":"ticketing","tables":[{"name":"tickets","columns":[]}],"auth":true}`)
	if err := AppendMessage(ctx, pool, session.ID, "assistant", "", plan); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	_, messages, err := GetOrCreateInProgressSession(ctx, pool, userA)
	if err != nil {
		t.Fatalf("GetOrCreateInProgressSession: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	var decoded map[string]any
	if err := json.Unmarshal(messages[0].Plan, &decoded); err != nil {
		t.Fatalf("unmarshal persisted plan: %v", err)
	}
	if decoded["name"] != "ticketing" {
		t.Errorf("expected persisted plan name %q, got %+v", "ticketing", decoded)
	}
}
