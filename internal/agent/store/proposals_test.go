package store_test

// proposals_test.go — ResolveProposal / ProposalDecision / RecordPublish
// (ai-frontend-builder T27 and T32). Same environment as store_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

const proposalArgs = `{"operation":{"kind":"add_column","add_column":{"table":"t","column":{"name":"c","type":"text"}}}}`

// awaiting leaves a turn of sess paused on a proposal, the way the loop does:
// the assistant message with the tool call, then FinishTurn(awaiting).
func (e *env) awaiting(t *testing.T, sess *store.Session) *store.Turn {
	t.Helper()
	ctx := context.Background()
	turn, lease := e.claimed(t, sess, "w1")
	calls := `[{"id":"call_1","name":"propose_backend_change","arguments":` + proposalArgs + `}]`
	if _, err := e.st.AppendMessage(ctx, lease, store.NewMessage{Role: store.RoleAssistant, ToolCalls: json.RawMessage(calls)}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	pending := json.RawMessage(`{"tool_call_id":"call_1","name":"propose_backend_change","arguments":` + proposalArgs + `}`)
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnAwaitingConfirmation, PendingProposal: pending}); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	got, err := e.st.GetTurn(ctx, turn.ID)
	if err != nil || got.Status != store.TurnAwaitingConfirmation {
		t.Fatalf("setup: turn not awaiting: %+v %v", got, err)
	}
	return got
}

// sameJSON compares JSON documents semantically (jsonb normalizes whitespace).
func sameJSON(got json.RawMessage, want string) bool {
	var a, b any
	return json.Unmarshal(got, &a) == nil && json.Unmarshal([]byte(want), &b) == nil && reflect.DeepEqual(a, b)
}

func confirmOK(content string) func(context.Context, store.PendingProposal) (*store.ProposalResolution, error) {
	return func(_ context.Context, p store.PendingProposal) (*store.ProposalResolution, error) {
		return &store.ProposalResolution{
			Content: content, EventType: store.EventProposalConfirmed,
			EventPayload: map[string]any{"tool_call_id": p.ToolCallID, "applied": true},
		}, nil
	}
}

func TestResolveProposal_ResumesTurnWithToolResultAndEvent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn := e.awaiting(t, sess)

	var seen store.PendingProposal
	got, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID,
		func(_ context.Context, p store.PendingProposal) (*store.ProposalResolution, error) {
			seen = p
			return &store.ProposalResolution{
				Content: "done", IsError: false, EventType: store.EventProposalConfirmed,
				EventPayload: map[string]any{"tool_call_id": p.ToolCallID, "applied": true},
			}, nil
		})
	if err != nil {
		t.Fatalf("ResolveProposal: %v", err)
	}
	if seen.ToolCallID != "call_1" || seen.Name != "propose_backend_change" || !sameJSON(seen.Arguments, proposalArgs) {
		t.Errorf("decide got %+v", seen)
	}
	if got.Status != store.TurnPending || len(got.PendingProposal) != 0 || got.LeaseOwner != nil || got.FinishedAt != nil {
		t.Errorf("turn not back to a clean pending: %+v", got)
	}

	msgs, err := e.st.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := msgs[len(msgs)-1]
	if last.Role != store.RoleTool || last.ToolCallID != "call_1" || last.Content != "done" || last.TurnID != turn.ID {
		t.Errorf("tool result not appended as the last message: %+v", last)
	}
	var meta struct {
		BuildFailed bool `json:"build_failed"`
		IsError     bool `json:"is_error"`
	}
	if err := json.Unmarshal(last.ToolCalls, &meta); err != nil || meta.IsError || meta.BuildFailed {
		t.Errorf("tool metadata = %s (%v)", last.ToolCalls, err)
	}

	evs, err := e.st.ListEvents(ctx, e.userA, sess.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ev := range evs {
		if ev.Type == store.EventProposalConfirmed && ev.TurnID != nil && *ev.TurnID == turn.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("no proposal_confirmed event on the turn: %+v", evs)
	}

	after, err := e.st.GetSession(ctx, e.userA, sess.ID)
	if err != nil || after.Status != store.SessionIdle {
		t.Errorf("session status = %q (%v), want idle", after.Status, err)
	}

	// A worker can claim it again.
	claimed, err := e.st.ClaimTurn(ctx, "w2", 30*time.Second)
	if err != nil || claimed.ID != turn.ID {
		t.Fatalf("resumed turn not claimable: %+v %v", claimed, err)
	}
}

func TestResolveProposal_ErrorResultIsMarked(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn := e.awaiting(t, sess)
	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID,
		func(_ context.Context, p store.PendingProposal) (*store.ProposalResolution, error) {
			return &store.ProposalResolution{Content: "nope", IsError: true, EventType: store.EventProposalRejected}, nil
		}); err != nil {
		t.Fatal(err)
	}
	msgs, _ := e.st.ListMessages(ctx, sess.ID)
	if !sameJSON(msgs[len(msgs)-1].ToolCalls, `{"build_failed":false,"is_error":true}`) {
		t.Errorf("tool metadata = %s", msgs[len(msgs)-1].ToolCalls)
	}
}

func TestResolveProposal_NotPendingAndForeignOwner(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)

	// A running turn is not awaiting.
	running, _ := e.claimed(t, sess, "w1")
	called := false
	decide := func(context.Context, store.PendingProposal) (*store.ProposalResolution, error) {
		called = true
		return nil, errors.New("must not run")
	}
	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, running.ID, decide); !errors.Is(err, store.ErrProposalNotPending) {
		t.Errorf("running turn: want ErrProposalNotPending, got %v", err)
	}

	// Another owner, and a turn that is not in that session, look the same.
	other := e.session(t, e.userB, e.appB)
	if _, err := e.st.ResolveProposal(ctx, e.userB, sess.ID, running.ID, decide); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign owner: want ErrNotFound, got %v", err)
	}
	if _, err := e.st.ResolveProposal(ctx, e.userB, other.ID, running.ID, decide); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("turn of another session: want ErrNotFound, got %v", err)
	}
	if called {
		t.Error("decide ran for a turn that cannot be resolved")
	}
}

func TestResolveProposal_DecideErrorRollsBackEverything(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn := e.awaiting(t, sess)
	before, _ := e.st.ListMessages(ctx, sess.ID)

	boom := errors.New("boom")
	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID,
		func(context.Context, store.PendingProposal) (*store.ProposalResolution, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("want the decide error back, got %v", err)
	}
	got, _ := e.st.GetTurn(ctx, turn.ID)
	if got.Status != store.TurnAwaitingConfirmation || len(got.PendingProposal) == 0 {
		t.Errorf("turn must still await confirmation: %+v", got)
	}
	after, _ := e.st.ListMessages(ctx, sess.ID)
	if len(after) != len(before) {
		t.Errorf("a message was written despite the error")
	}
	// ... and the user can still resolve it.
	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID, confirmOK("ok")); err != nil {
		t.Errorf("retry after a failed decide: %v", err)
	}
}

func TestResolveProposal_ConcurrentResolutionsDecideOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn := e.awaiting(t, sess)

	var decided atomic.Int32
	var ok, notPending atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID,
				func(_ context.Context, p store.PendingProposal) (*store.ProposalResolution, error) {
					decided.Add(1)
					time.Sleep(100 * time.Millisecond) // the DDL
					return &store.ProposalResolution{Content: "x", EventType: store.EventProposalConfirmed}, nil
				})
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, store.ErrProposalNotPending), errors.Is(err, store.ErrProposalBusy):
				notPending.Add(1) // lost the race: not pending yet, or busy right now
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if decided.Load() != 1 || ok.Load() != 1 || notPending.Load() != 7 {
		t.Errorf("decided=%d ok=%d notPending=%d, want 1/1/7", decided.Load(), ok.Load(), notPending.Load())
	}
	msgs, _ := e.st.ListMessages(ctx, sess.ID)
	tools := 0
	for _, m := range msgs {
		if m.Role == store.RoleTool {
			tools++
		}
	}
	if tools != 1 {
		t.Errorf("%d tool results written, want 1", tools)
	}
}

func TestResolveProposal_ArchivedSessionAndBadResolution(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn := e.awaiting(t, sess)

	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID,
		func(context.Context, store.PendingProposal) (*store.ProposalResolution, error) {
			return &store.ProposalResolution{Content: "x"}, nil // no event type
		}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("missing event type: want ErrInvalidArgument, got %v", err)
	}

	if err := e.st.ArchiveSession(ctx, e.userA, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID, confirmOK("x")); !errors.Is(err, store.ErrSessionArchived) {
		t.Errorf("archived session: want ErrSessionArchived, got %v", err)
	}
}

func TestProposalDecision(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn := e.awaiting(t, sess)

	if typ, _, err := e.st.ProposalDecision(ctx, turn.ID); err != nil || typ != "" {
		t.Fatalf("before resolving: %q %v", typ, err)
	}
	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID, confirmOK("ok")); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := e.st.ProposalDecision(ctx, turn.ID)
	if err != nil || typ != store.EventProposalConfirmed {
		t.Fatalf("after resolving: %q %v", typ, err)
	}
	var p struct {
		Applied bool `json:"applied"`
	}
	if json.Unmarshal(payload, &p) != nil || !p.Applied {
		t.Errorf("payload = %s", payload)
	}
}

func TestParsePendingProposal(t *testing.T) {
	if _, ok := store.ParsePendingProposal(nil); ok {
		t.Error("nil must not parse")
	}
	if _, ok := store.ParsePendingProposal(json.RawMessage(`{"name":"x"}`)); ok {
		t.Error("a proposal without a tool call id must not parse")
	}
	p, ok := store.ParsePendingProposal(json.RawMessage(`{"tool_call_id":"c","name":"n","arguments":{"a":1}}`))
	if !ok || p.ToolCallID != "c" || string(p.Arguments) != `{"a":1}` {
		t.Errorf("got %+v %v", p, ok)
	}
}

func TestRecordPublish(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	sha1 := "1111111111111111111111111111111111111111"
	sha2 := "2222222222222222222222222222222222222222"

	var tplID, faID string
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates (name, github_owner, github_repo, created_by) VALUES ('t','o','r','x') RETURNING id`).Scan(&tplID); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (name, slug, template_id, created_by) VALUES ('n','rp-slug',$1,'x') RETURNING id`, tplID).Scan(&faID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM zeep_system.builder_sessions WHERE id = $1`, sess.ID)
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM zeep_system.frontend_apps WHERE id = $1`, faID)
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM zeep_system.github_templates WHERE id = $1`, tplID)
	})

	if err := e.st.RecordPublish(ctx, e.userA, sess.ID, &faID, sha1); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	got, _ := e.st.GetSession(ctx, e.userA, sess.ID)
	if got.FrontendAppID == nil || *got.FrontendAppID != faID || got.PublishedSHA != sha1 {
		t.Errorf("after first publish: %+v", got)
	}
	// A republish keeps the link and moves the sha.
	if err := e.st.RecordPublish(ctx, e.userA, sess.ID, nil, sha2); err != nil {
		t.Fatalf("republish: %v", err)
	}
	got, _ = e.st.GetSession(ctx, e.userA, sess.ID)
	if got.FrontendAppID == nil || *got.FrontendAppID != faID || got.PublishedSHA != sha2 {
		t.Errorf("after republish: %+v", got)
	}

	if err := e.st.RecordPublish(ctx, e.userB, sess.ID, nil, sha1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign owner: want ErrNotFound, got %v", err)
	}
	if err := e.st.RecordPublish(ctx, e.userA, sess.ID, nil, ""); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("empty sha: want ErrInvalidArgument, got %v", err)
	}
	if again, _ := e.st.GetSession(ctx, e.userA, sess.ID); again.PublishedSHA != sha2 {
		t.Errorf("rejected calls changed the sha: %q", again.PublishedSHA)
	}
}

// A second resolver never waits for the first one (and so never holds a pool
// connection while the first needs another for its DDL): it gets ErrProposalBusy
// at once.
func TestResolveProposal_ConcurrentResolverGetsBusyWithoutWaiting(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn := e.awaiting(t, sess)

	inDecide, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID,
			func(context.Context, store.PendingProposal) (*store.ProposalResolution, error) {
				close(inDecide)
				<-release
				return &store.ProposalResolution{Content: "x", EventType: store.EventProposalConfirmed}, nil
			})
		done <- err
	}()
	<-inDecide

	start := time.Now()
	_, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID, confirmOK("second"))
	if !errors.Is(err, store.ErrProposalBusy) {
		t.Errorf("second resolver: want ErrProposalBusy, got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("second resolver waited %v", d)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first resolver: %v", err)
	}
	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID, confirmOK("third")); !errors.Is(err, store.ErrProposalNotPending) {
		t.Errorf("after the first finished: want ErrProposalNotPending, got %v", err)
	}
}
