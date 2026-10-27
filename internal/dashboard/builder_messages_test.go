package dashboard

// builder_messages_test.go — ai-frontend-builder T26: POST .../messages and
// GET .../events. Integration tests against real Postgres (TEST_DATABASE_URL)
// with the fake sandbox of builder_handlers_test.go; the handlers are called
// directly. The event payload whitelist also has pure unit tests.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// bhWithPoke wires a poke counter into the handler.
func (e *bhEnv) withPoke() *atomic.Int32 {
	var n atomic.Int32
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.fake, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example",
		Poke: func() { n.Add(1) },
	})
	return &n
}

func (e *bhEnv) message(t *testing.T, user *DashboardUser, sessionID, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, e.h.CreateMessage, user, http.MethodPost,
		"/dashboard/api/builder/sessions/"+sessionID+"/messages", body, "id", sessionID)
}

func (e *bhEnv) events(t *testing.T, user *DashboardUser, sessionID, query string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/dashboard/api/builder/sessions/" + sessionID + "/events"
	if query != "" {
		target += "?" + query
	}
	return e.do(t, e.h.ListEvents, user, http.MethodGet, target, "", "id", sessionID)
}

func (e *bhEnv) turnCount(t *testing.T, sessionID string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.builder_turns WHERE session_id = $1`, sessionID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// finishTurn runs a pending turn to completion through the store, the way a
// worker would: claim, optionally snapshot a commit, finish.
func (e *bhEnv) finishTurn(t *testing.T, status, commitSHA string) *store.Turn {
	t.Helper()
	ctx := context.Background()
	turn, err := e.st.ClaimTurn(ctx, "test-worker", time.Minute)
	if err != nil {
		t.Fatalf("claim turn: %v", err)
	}
	lease := store.Lease{TurnID: turn.ID, Owner: "test-worker"}
	if commitSHA != "" {
		if err := e.st.SaveSnapshot(ctx, lease, commitSHA, []byte("bundle-"+commitSHA)); err != nil {
			t.Fatalf("save snapshot: %v", err)
		}
	}
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: status, CommitSHA: commitSHA}); err != nil {
		t.Fatalf("finish turn: %v", err)
	}
	return turn
}

func TestBuilderMessage_Returns202WithPendingTurnAndPokesWorker(t *testing.T) {
	e := newBHEnv(t)
	pokes := e.withPoke()
	sess := e.createOK(t, e.viewer, e.appA)

	w := e.message(t, e.viewer, sess.ID, `{"content":"  a page listing tasks  "}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderTurnView](t, w)
	if got.ID == "" || got.Seq != 1 || got.Kind != store.KindChat || got.Status != store.TurnPending {
		t.Fatalf("turn = %+v, want a pending chat turn with seq 1", got)
	}
	if pokes.Load() != 1 {
		t.Errorf("worker pokes = %d, want 1", pokes.Load())
	}
	msgs, err := e.st.ListMessages(context.Background(), sess.ID)
	if err != nil || len(msgs) != 1 || msgs[0].Role != store.RoleUser || msgs[0].Content != "a page listing tasks" || msgs[0].TurnID != got.ID {
		t.Fatalf("messages = %+v err=%v, want the trimmed user message on the new turn", msgs, err)
	}
	// The audit row says a turn was requested, never what was said.
	var meta string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT metadata::text FROM zeep_system.audit_log WHERE action = 'builder.message.create' AND resource_id = $1`, sess.ID).Scan(&meta); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if strings.Contains(meta, "tasks") {
		t.Errorf("chat content leaked into the audit log: %s", meta)
	}
}

func TestBuilderMessage_WithoutPokeStillEnqueues(t *testing.T) {
	e := newBHEnv(t) // no Poke configured
	sess := e.createOK(t, e.viewer, e.appA)
	if w := e.message(t, e.viewer, sess.ID, `{"content":"hi"}`); w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBuilderMessage_SecondMessageWhileTurnActiveIs409(t *testing.T) {
	e := newBHEnv(t)
	pokes := e.withPoke()
	sess := e.createOK(t, e.viewer, e.appA)
	if w := e.message(t, e.viewer, sess.ID, `{"content":"first"}`); w.Code != http.StatusAccepted {
		t.Fatalf("first: %d %s", w.Code, w.Body.String())
	}

	w := e.message(t, e.viewer, sess.ID, `{"content":"second"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := bhErrorBody(t, w); got != "a turn is already in progress" {
		t.Errorf("error = %q", got)
	}
	if e.turnCount(t, sess.ID) != 1 {
		t.Error("the rejected message must not create a turn")
	}
	if msgs, _ := e.st.ListMessages(context.Background(), sess.ID); len(msgs) != 1 {
		t.Errorf("messages = %d, the rejected message must not be stored", len(msgs))
	}
	if pokes.Load() != 1 {
		t.Errorf("pokes = %d, a rejected message must not poke", pokes.Load())
	}

	// A paused turn (waiting for a proposal decision) is active too.
	ctx := context.Background()
	turn, err := e.st.ClaimTurn(ctx, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.FinishTurn(ctx, store.Lease{TurnID: turn.ID, Owner: "w"}, store.FinishParams{
		Status: store.TurnAwaitingConfirmation, PendingProposal: json.RawMessage(`{"tool_call_id":"c1"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if w := e.message(t, e.viewer, sess.ID, `{"content":"third"}`); w.Code != http.StatusConflict {
		t.Errorf("message while awaiting confirmation: want 409, got %d", w.Code)
	}
}

func TestBuilderMessage_NextMessageAfterTurnFinishedAndOnFailedSession(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)
	e.message(t, e.viewer, sess.ID, `{"content":"one"}`)
	e.finishTurn(t, store.TurnFailed, "")
	if got := e.sessionStatus(t, sess.ID); got != store.SessionFailed {
		t.Fatalf("session status = %s, want failed", got)
	}

	w := e.message(t, e.viewer, sess.ID, `{"content":"retry"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("a failed session still accepts messages: got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[BuilderTurnView](t, w); got.Seq != 2 {
		t.Errorf("seq = %d, want 2", got.Seq)
	}
}

func TestBuilderMessage_OwnerOnlyAndAuth(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)

	// Another user, even an admin of the same backend app, sees a 404.
	for name, u := range map[string]*DashboardUser{"outsider": e.outsider, "app admin": e.owner, "superadmin": e.super} {
		if w := e.message(t, u, sess.ID, `{"content":"x"}`); w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d: %s", name, w.Code, w.Body.String())
		}
	}
	if w := e.message(t, e.viewer, "11111111-1111-1111-1111-111111111111", `{"content":"x"}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown session: want 404, got %d", w.Code)
	}
	if w := e.message(t, e.viewer, "not-a-uuid", `{"content":"x"}`); w.Code != http.StatusNotFound {
		t.Errorf("malformed id: want 404, got %d", w.Code)
	}
	if w := e.message(t, nil, sess.ID, `{"content":"x"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("no user: want 401, got %d", w.Code)
	}
	if e.turnCount(t, sess.ID) != 0 {
		t.Error("no turn may be created by a rejected request")
	}

	// An archived session is gone for messages too.
	if w := e.del(t, e.viewer, sess.ID); w.Code != http.StatusNoContent {
		t.Fatal("delete failed")
	}
	if w := e.message(t, e.viewer, sess.ID, `{"content":"x"}`); w.Code != http.StatusNotFound {
		t.Errorf("archived: want 404, got %d", w.Code)
	}
}

func TestBuilderMessage_ValidationAndCaps(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)

	tooLong := `{"content":"` + strings.Repeat("é", builderMaxMessageRunes+1) + `"}`
	atLimit := `{"content":"` + strings.Repeat("é", builderMaxMessageRunes) + `"}`
	hugeBody := `{"content":"` + strings.Repeat("a", builderMessageBodyLimit) + `"}`
	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty content", `{"content":""}`, http.StatusBadRequest},
		{"blank content", `{"content":"  \n\t "}`, http.StatusBadRequest},
		{"missing content", `{}`, http.StatusBadRequest},
		{"not json", `nope`, http.StatusBadRequest},
		{"wrong type", `{"content":123}`, http.StatusBadRequest},
		{"NUL character", `{"content":"a\u0000b"}`, http.StatusBadRequest},
		{"too long", tooLong, http.StatusBadRequest},
		{"body over the cap", hugeBody, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		if w := e.message(t, e.viewer, sess.ID, tc.body); w.Code != tc.want {
			t.Errorf("%s: want %d, got %d: %s", tc.name, tc.want, w.Code, w.Body.String())
		}
	}
	if e.turnCount(t, sess.ID) != 0 {
		t.Error("invalid messages must not create turns")
	}
	if w := e.message(t, e.viewer, sess.ID, atLimit); w.Code != http.StatusAccepted {
		t.Errorf("a message of exactly the cap must pass: got %d: %s", w.Code, w.Body.String())
	}
}

func TestBuilderMessage_BuilderNotAvailableIs503Generic(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)

	e.fake.mu.Lock()
	e.fake.healthErr = fmt.Errorf("dial tcp builder.internal:8080: refused")
	e.fake.mu.Unlock()
	w := e.message(t, e.viewer, sess.ID, `{"content":"x"}`)
	if w.Code != http.StatusServiceUnavailable || bhErrorBody(t, w) != "builder unavailable" || strings.Contains(w.Body.String(), "builder.internal") {
		t.Fatalf("want a generic 503, got %d: %s", w.Code, w.Body.String())
	}
	if e.turnCount(t, sess.ID) != 0 {
		t.Error("an unavailable builder must not accept turns")
	}

	e2 := newBHEnv(t, bhWithoutBuilder())
	if w := e2.message(t, e2.viewer, "11111111-1111-1111-1111-111111111111", `{"content":"x"}`); w.Code != http.StatusServiceUnavailable {
		t.Errorf("not configured: want 503, got %d", w.Code)
	}
}

// ---- events ----

func TestBuilderEvents_AfterCursorOrderedAndPaged(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)
	ctx := context.Background()
	var ids []int64
	for i := 1; i <= 5; i++ {
		id, err := e.st.AppendSessionEvent(ctx, sess.ID, "", "tool_call", map[string]any{"name": "write_file", "summary": fmt.Sprintf("src/f%d.ts", i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	w := e.events(t, e.viewer, sess.ID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	all := decodeJSON[BuilderEventsPage](t, w)
	if len(all.Events) != 5 || all.NextCursor != ids[4] {
		t.Fatalf("page = %d events, next %d; want 5 and %d", len(all.Events), all.NextCursor, ids[4])
	}
	for i, ev := range all.Events {
		if ev.ID != ids[i] || ev.Type != "tool_call" || !strings.Contains(string(ev.Payload), fmt.Sprintf("src/f%d.ts", i+1)) {
			t.Errorf("event %d = %+v, want id %d in insertion order", i, ev, ids[i])
		}
	}

	// after = a cursor returns only newer events, oldest first.
	page := decodeJSON[BuilderEventsPage](t, e.events(t, e.viewer, sess.ID, fmt.Sprintf("after=%d", ids[1])))
	if len(page.Events) != 3 || page.Events[0].ID != ids[2] || page.NextCursor != ids[4] {
		t.Errorf("after page = %+v", page)
	}
	// limit pages; following next_cursor walks the whole log.
	var walked []int64
	cursor := int64(0)
	for range 10 {
		p := decodeJSON[BuilderEventsPage](t, e.events(t, e.viewer, sess.ID, fmt.Sprintf("after=%d&limit=2", cursor)))
		if len(p.Events) == 0 {
			break
		}
		if len(p.Events) > 2 {
			t.Fatalf("limit=2 returned %d events", len(p.Events))
		}
		for _, ev := range p.Events {
			walked = append(walked, ev.ID)
		}
		cursor = p.NextCursor
	}
	if fmt.Sprint(walked) != fmt.Sprint(ids) {
		t.Errorf("walked ids = %v, want %v", walked, ids)
	}
	// Nothing new: empty list (never null) and the cursor is unchanged.
	empty := e.events(t, e.viewer, sess.ID, fmt.Sprintf("after=%d", ids[4]))
	if !strings.Contains(empty.Body.String(), `"events":[]`) {
		t.Errorf("empty page body = %s, want an empty array", empty.Body.String())
	}
	if p := decodeJSON[BuilderEventsPage](t, empty); p.NextCursor != ids[4] {
		t.Errorf("next_cursor = %d, want it to stay %d", p.NextCursor, ids[4])
	}
}

func TestBuilderEvents_PageSizeIsCapped(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)
	ctx := context.Background()
	for i := 0; i < builderEventsMaxLimit+10; i++ {
		if _, err := e.st.AppendSessionEvent(ctx, sess.ID, "", "queued", nil); err != nil {
			t.Fatal(err)
		}
	}
	if p := decodeJSON[BuilderEventsPage](t, e.events(t, e.viewer, sess.ID, "")); len(p.Events) != builderEventsDefaultLimit {
		t.Errorf("default page = %d events, want %d", len(p.Events), builderEventsDefaultLimit)
	}
	if p := decodeJSON[BuilderEventsPage](t, e.events(t, e.viewer, sess.ID, "limit=100000")); len(p.Events) != builderEventsMaxLimit {
		t.Errorf("limit=100000 gave %d events, want the cap %d", len(p.Events), builderEventsMaxLimit)
	}
}

func TestBuilderEvents_OwnerOnlyAndValidation(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)

	for name, u := range map[string]*DashboardUser{"outsider": e.outsider, "app admin": e.owner, "superadmin": e.super} {
		if w := e.events(t, u, sess.ID, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", name, w.Code)
		}
	}
	if w := e.events(t, nil, sess.ID, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no user: want 401, got %d", w.Code)
	}
	if w := e.events(t, e.viewer, "nope", ""); w.Code != http.StatusNotFound {
		t.Errorf("malformed id: want 404, got %d", w.Code)
	}
	for _, q := range []string{"after=abc", "after=-1", "limit=0", "limit=-5", "limit=x", "after=99999999999999999999"} {
		if w := e.events(t, e.viewer, sess.ID, q); w.Code != http.StatusBadRequest {
			t.Errorf("%q: want 400, got %d: %s", q, w.Code, w.Body.String())
		}
	}
	e2 := newBHEnv(t, bhWithoutBuilder())
	if w := e2.events(t, e2.viewer, "11111111-1111-1111-1111-111111111111", ""); w.Code != http.StatusServiceUnavailable {
		t.Errorf("not configured: want 503, got %d", w.Code)
	}
}

func TestBuilderEvents_PayloadsNeverCarryFileContentsOrProviderErrors(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)
	ctx := context.Background()
	// Hostile payloads: what a buggy or future loop might store.
	for typ, payload := range map[string]any{
		"tool_call":      map[string]any{"name": "write_file", "summary": "src/a.ts", "arguments": map[string]any{"content": "FILE-BODY-1"}, "content": "FILE-BODY-2"},
		"file_changed":   map[string]any{"path": "src/a.ts", "op": "write", "content": "FILE-BODY-3", "diff": "FILE-BODY-4"},
		"error":          map[string]any{"code": "provider_unavailable", "message": "sk-secret-key rejected by api.openai.example", "err": "dial tcp 10.0.0.1"},
		"build_failed":   map[string]any{"step": "build", "tail": "error TS2304", "env": map[string]any{"TOKEN": "tok-secret"}},
		"publish_failed": map[string]any{"code": "render_error", "detail": "render said: tok-secret-2", "body": "x", "nested": map[string]any{"a": "NESTED-SECRET"}},
		"future_event":   map[string]any{"id": 7, "ok": true, "label": "fine", "content": "FILE-BODY-5", "list": []string{"LIST-SECRET"}},
	} {
		if _, err := e.st.AppendSessionEvent(ctx, sess.ID, "", typ, payload); err != nil {
			t.Fatal(err)
		}
	}
	w := e.events(t, e.viewer, sess.ID, "")
	body := w.Body.String()
	for _, secret := range []string{"FILE-BODY", "sk-secret", "api.openai.example", "10.0.0.1", "tok-secret", "NESTED-SECRET", "LIST-SECRET", "render said"} {
		if strings.Contains(body, secret) {
			t.Errorf("response leaks %q: %s", secret, body)
		}
	}
	page := decodeJSON[BuilderEventsPage](t, w)
	by := map[string]string{}
	for _, ev := range page.Events {
		by[ev.Type] = string(ev.Payload)
	}
	wantPayload := map[string]string{
		"tool_call":      `{"name":"write_file","summary":"src/a.ts"}`,
		"file_changed":   `{"op":"write","path":"src/a.ts"}`,
		"error":          `{"code":"provider_unavailable"}`,
		"build_failed":   `{"step":"build","tail":"error TS2304"}`,
		"publish_failed": `{"code":"render_error"}`,
		"future_event":   `{"id":7,"label":"fine","ok":true}`,
	}
	for typ, want := range wantPayload {
		if by[typ] != want {
			t.Errorf("%s payload = %s, want %s", typ, by[typ], want)
		}
	}
}

// ---- pure unit tests of the whitelist ----

func TestSanitizeBuilderEventPayload(t *testing.T) {
	long := strings.Repeat("x", 5000)
	cases := []struct {
		name, typ, in, want string
	}{
		{"turn_started", "turn_started", `{"seq":2,"resumed":false,"kind":"revert","x":1}`, `{"kind":"revert","resumed":false,"seq":2}`},
		{"wrong types are dropped", "turn_started", `{"seq":"two","resumed":"no"}`, `{}`},
		{"assistant message kept", "assistant_message", `{"content":"hello"}`, `{"content":"hello"}`},
		{"revert", "revert", `{"to_turn_seq":1,"sha":"abc","changed":true,"build":"failed","raw":"x"}`, `{"build":"failed","changed":true,"sha":"abc","to_turn_seq":1}`},
		{"proposal object kept", "proposal", `{"tool_call_id":"c1","name":"propose","operation":{"op":"add_column"},"arguments":"x"}`, `{"name":"propose","operation":{"op":"add_column"},"tool_call_id":"c1"}`},
		{"proposal non-object dropped", "proposal", `{"operation":"free text"}`, `{}`},
		{"budget_exceeded has no fields", "budget_exceeded", `{"message":"cap"}`, `{}`},
		{"queued", "queued", `{}`, `{}`},
		{"null payload", "turn_finished", `null`, `{}`},
		{"empty payload", "turn_finished", ``, `{}`},
		{"garbage payload", "turn_finished", `[1,2]`, `{}`},
		{"unknown type keeps short scalars", "publish_succeeded", `{"url":"https://x.example","n":3,"obj":{"a":1},"content":"c"}`, `{"n":3,"url":"https://x.example"}`},
		{"unknown type drops odd keys", "whatever", `{"Bad-Key":"x","ok":true}`, `{"ok":true}`},
	}
	for _, tc := range cases {
		if got := string(sanitizeBuilderEventPayload(tc.typ, json.RawMessage(tc.in))); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}

	// Long strings are capped, not dropped.
	got := string(sanitizeBuilderEventPayload("build_failed", json.RawMessage(`{"step":"build","tail":"`+long+`"}`)))
	var m map[string]string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(m["tail"])); n != maxEventTailString+1 { // + the ellipsis
		t.Errorf("tail length = %d, want %d", n, maxEventTailString+1)
	}
	got = string(sanitizeBuilderEventPayload("whatever", json.RawMessage(`{"label":"`+long+`"}`)))
	_ = json.Unmarshal([]byte(got), &m)
	if n := len([]rune(m["label"])); n != maxEventShortString+1 {
		t.Errorf("unknown-type string length = %d, want %d", n, maxEventShortString+1)
	}
	// An oversized proposal operation is dropped.
	big := `{"operation":{"a":"` + strings.Repeat("y", maxEventObjectBytes) + `"}}`
	if got := string(sanitizeBuilderEventPayload("proposal", json.RawMessage(big))); got != `{}` {
		t.Errorf("oversized operation kept: %.60s", got)
	}
}
