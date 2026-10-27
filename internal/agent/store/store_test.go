package store_test

// store_test.go — integration tests for the builder store against a real
// Postgres (TEST_DATABASE_URL, no DB mocking). Derived from the "Done when"
// list of ai-frontend-builder T12 and the design's turn lease semantics, not
// from reading the implementation. The external test package is required:
// dashboard will import store, so an in-package test importing dashboard
// (for ProvisionZeepSystem) would be an import cycle.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type env struct {
	pool  *db.Pool
	st    *store.Store
	userA string
	userB string
	appA  string
	appB  string
}

func resetBuilderTables(pool *db.Pool) {
	_, _ = pool.Exec(context.Background(),
		`TRUNCATE zeep_system.builder_sessions, zeep_system.builder_settings, zeep_system.ai_usage_ledger CASCADE`)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := dashboard.ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}
	// The claim queue is global, so start from an empty builder state.
	resetBuilderTables(pool)

	suffix := time.Now().UnixNano()
	mkUser := func(label string) string {
		u, err := dashboard.CreateUser(ctx, pool, fmt.Sprintf("builder-store-%s-%d@example.com", label, suffix), label, "hash", "member")
		if err != nil {
			t.Fatalf("create user %s: %v", label, err)
		}
		return u.ID
	}
	mkApp := func(label, owner string) string {
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
			fmt.Sprintf("builder-store-%s-%d", label, suffix), owner,
		).Scan(&id)
		if err != nil {
			t.Fatalf("create app %s: %v", label, err)
		}
		return id
	}
	e := &env{pool: pool, st: store.New(pool)}
	e.userA, e.userB = mkUser("a"), mkUser("b")
	e.appA, e.appB = mkApp("a", e.userA), mkApp("b", e.userB)

	t.Cleanup(func() {
		resetBuilderTables(pool)
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM zeep_system.apps WHERE id = ANY($1::uuid[])`, []string{e.appA, e.appB})
		_, _ = pool.Exec(bg, `DELETE FROM zeep_system.dashboard_users WHERE id = ANY($1::uuid[])`, []string{e.userA, e.userB})
	})
	return e
}

func (e *env) session(t *testing.T, owner, app string) *store.Session {
	t.Helper()
	s, err := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID:     owner,
		BackendAppID:    app,
		Title:           "t",
		TemplateKind:    store.TemplateDefault,
		TemplateVersion: "v0",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return s
}

// claimed creates a pending turn on sess and claims it as owner.
func (e *env) claimed(t *testing.T, sess *store.Session, owner string) (*store.Turn, store.Lease) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.st.CreateTurn(ctx, sess.OwnerUserID, sess.ID, store.KindChat, "build me an app"); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	turn, err := e.st.ClaimTurn(ctx, owner, 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimTurn: %v", err)
	}
	return turn, store.Lease{TurnID: turn.ID, Owner: owner}
}

func (e *env) expireLease(t *testing.T, turnID string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.builder_turns SET lease_until = now() - interval '1 second' WHERE id = $1`, turnID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
}

// Sessions are owner-scoped: another user gets ErrNotFound for get, list,
// turns, events and archive (AFB-04/AFB-06 ownership).
func TestSessions_OwnerScoping(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	other := e.session(t, e.userB, e.appB)

	got, err := e.st.GetSession(ctx, e.userA, sess.ID)
	if err != nil {
		t.Fatalf("owner GetSession: %v", err)
	}
	if got.OwnerUserID != e.userA || got.Status != store.SessionIdle || got.Locale != "en" || got.HeadTurnSeq != 0 {
		t.Errorf("unexpected defaults: %+v", got)
	}

	if _, err := e.st.GetSession(ctx, e.userB, sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign GetSession: want ErrNotFound, got %v", err)
	}
	if _, err := e.st.ListTurns(ctx, e.userB, sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign ListTurns: want ErrNotFound, got %v", err)
	}
	if _, err := e.st.ListEvents(ctx, e.userB, sess.ID, 0, 10); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign ListEvents: want ErrNotFound, got %v", err)
	}
	if _, err := e.st.CreateTurn(ctx, e.userB, sess.ID, store.KindChat, "hi"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign CreateTurn: want ErrNotFound, got %v", err)
	}
	if err := e.st.ArchiveSession(ctx, e.userB, sess.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign ArchiveSession: want ErrNotFound, got %v", err)
	}

	list, err := e.st.ListSessions(ctx, e.userA)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 1 || list[0].ID != sess.ID {
		t.Errorf("ListSessions(A) must contain only A's session, got %+v", list)
	}
	if other.OwnerUserID != e.userB {
		t.Fatalf("setup: other session owner mismatch")
	}
}

// ListSessions orders by updated_at DESC and hides archived sessions.
func TestSessions_ListOrderAndArchive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s1 := e.session(t, e.userA, e.appA)
	s2 := e.session(t, e.userA, e.appA)
	s3 := e.session(t, e.userA, e.appA)

	if _, err := e.pool.Exec(ctx,
		`UPDATE zeep_system.builder_sessions SET updated_at = now() + interval '1 hour' WHERE id = $1`, s1.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ArchiveSession(ctx, e.userA, s3.ID); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}
	if err := e.st.ArchiveSession(ctx, e.userA, s3.ID); err != nil {
		t.Errorf("ArchiveSession must be idempotent: %v", err)
	}

	list, err := e.st.ListSessions(ctx, e.userA)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != s1.ID || list[1].ID != s2.ID {
		t.Errorf("want [s1 s2] (updated_at desc, archived hidden), got %+v", list)
	}
	if _, err := e.st.CreateTurn(ctx, e.userA, s3.ID, store.KindChat, "x"); !errors.Is(err, store.ErrSessionArchived) {
		t.Errorf("CreateTurn on archived session: want ErrSessionArchived, got %v", err)
	}
}

// AFB-18: a second non-terminal turn on the same session violates the
// ListTurns attaches each turn's user message and leaves revert turns without one.
func TestListTurns_UserMessage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)

	t1, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "make a landing page")
	if err != nil {
		t.Fatal(err)
	}
	turns, err := e.st.ListTurns(ctx, e.userA, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].ID != t1.ID || turns[0].UserMessage != "make a landing page" {
		t.Errorf("unexpected turns: %+v", turns)
	}
}

// one-active-turn constraint -> ErrTurnActive, for every non-terminal status;
// once the turn finishes a new one is accepted with the next seq.
func TestCreateTurn_OneActiveTurnViolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)

	t1, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "first")
	if err != nil {
		t.Fatalf("first CreateTurn: %v", err)
	}
	if t1.Seq != 1 || t1.Status != store.TurnPending || t1.Kind != store.KindChat {
		t.Errorf("unexpected first turn: %+v", t1)
	}
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "second"); !errors.Is(err, store.ErrTurnActive) {
		t.Fatalf("second CreateTurn while pending: want ErrTurnActive, got %v", err)
	}

	// Still blocked while queued, running, and awaiting confirmation.
	if _, err := e.st.MarkQueued(ctx, t1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "x"); !errors.Is(err, store.ErrTurnActive) {
		t.Errorf("while queued: want ErrTurnActive, got %v", err)
	}
	claimed, err := e.st.ClaimTurn(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "x"); !errors.Is(err, store.ErrTurnActive) {
		t.Errorf("while running: want ErrTurnActive, got %v", err)
	}
	l := store.Lease{TurnID: claimed.ID, Owner: "w1"}
	if err := e.st.FinishTurn(ctx, l, store.FinishParams{
		Status: store.TurnAwaitingConfirmation, PendingProposal: json.RawMessage(`{"tool_call_id":"c1"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "x"); !errors.Is(err, store.ErrTurnActive) {
		t.Errorf("while awaiting_confirmation: want ErrTurnActive, got %v", err)
	}

	// A different session of the same user is unaffected.
	other := e.session(t, e.userA, e.appA)
	if _, err := e.st.CreateTurn(ctx, e.userA, other.ID, store.KindChat, "ok"); err != nil {
		t.Errorf("turn on another session must succeed: %v", err)
	}
}

// Terminal turns release the session: the next turn gets seq+1 and the user
// message is stored with it.
func TestCreateTurn_AfterTerminalGetsNextSeqAndStoresMessage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, lease := e.claimed(t, sess, "w1")
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnCompleted, CommitSHA: "sha1"}); err != nil {
		t.Fatal(err)
	}

	t2, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "second message")
	if err != nil {
		t.Fatalf("CreateTurn after terminal: %v", err)
	}
	if t2.Seq != turn.Seq+1 {
		t.Errorf("want seq %d, got %d", turn.Seq+1, t2.Seq)
	}
	msgs, err := e.st.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Role != store.RoleUser || msgs[1].Content != "second message" || msgs[1].TurnID != t2.ID {
		t.Errorf("user messages not stored with their turn in order: %+v", msgs)
	}
}

// Concurrent CreateTurn on one session: exactly one wins, the rest get
// ErrTurnActive, and seq is never duplicated.
func TestCreateTurn_ConcurrentOnlyOneWins(t *testing.T) {
	e := newEnv(t)
	sess := e.session(t, e.userA, e.appA)

	const n = 8
	var wins, active atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.st.CreateTurn(context.Background(), e.userA, sess.ID, store.KindChat, "hi")
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, store.ErrTurnActive):
				active.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || active.Load() != n-1 {
		t.Errorf("want 1 winner and %d ErrTurnActive, got %d and %d", n-1, wins.Load(), active.Load())
	}
}

// AFB-18: two concurrent claims of a single pending turn -> exactly one wins
// (FOR UPDATE SKIP LOCKED); the loser sees nothing to claim.
func TestClaimTurn_ConcurrentClaimsOneWins(t *testing.T) {
	e := newEnv(t)
	sess := e.session(t, e.userA, e.appA)
	if _, err := e.st.CreateTurn(context.Background(), e.userA, sess.ID, store.KindChat, "go"); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	var wins, none atomic.Int32
	owners := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := fmt.Sprintf("replica-%d", i)
			turn, err := e.st.ClaimTurn(context.Background(), owner, 30*time.Second)
			switch {
			case err == nil:
				wins.Add(1)
				if turn.Status != store.TurnRunning || turn.LeaseOwner == nil || *turn.LeaseOwner != owner || turn.LeaseUntil == nil {
					t.Errorf("claimed turn has no/foreign lease: %+v", turn)
				}
				owners <- owner
			case errors.Is(err, store.ErrNoTurn):
				none.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	close(owners)
	if wins.Load() != 1 || none.Load() != n-1 {
		t.Fatalf("want exactly 1 winner and %d ErrNoTurn, got %d and %d", n-1, wins.Load(), none.Load())
	}
	winner := <-owners
	var dbOwner string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT lease_owner FROM zeep_system.builder_turns WHERE session_id = $1`, sess.ID).Scan(&dbOwner); err != nil {
		t.Fatal(err)
	}
	if dbOwner != winner {
		t.Errorf("persisted lease owner %q != winner %q", dbOwner, winner)
	}
	if s, _ := e.st.GetSession(context.Background(), e.userA, sess.ID); s == nil || s.Status != store.SessionRunning {
		t.Errorf("session should be running after claim, got %+v", s)
	}
}

// Claim order is FIFO by created_at, and queued turns are claimable.
func TestClaimTurn_FIFOAndQueuedClaimable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s1 := e.session(t, e.userA, e.appA)
	s2 := e.session(t, e.userA, e.appA)
	t1, err := e.st.CreateTurn(ctx, e.userA, s1.ID, store.KindChat, "one")
	if err != nil {
		t.Fatal(err)
	}
	t2, err := e.st.CreateTurn(ctx, e.userA, s2.ID, store.KindChat, "two")
	if err != nil {
		t.Fatal(err)
	}
	// Make FIFO unambiguous regardless of clock resolution.
	if _, err := e.pool.Exec(ctx,
		`UPDATE zeep_system.builder_turns SET created_at = now() - interval '1 minute' WHERE id = $1`, t1.ID); err != nil {
		t.Fatal(err)
	}
	if changed, err := e.st.MarkQueued(ctx, t1.ID); err != nil || !changed {
		t.Fatalf("MarkQueued: changed=%v err=%v", changed, err)
	}

	first, err := e.st.ClaimTurn(ctx, "w", 30*time.Second)
	if err != nil || first.ID != t1.ID {
		t.Fatalf("want queued oldest turn %s first, got %+v err=%v", t1.ID, first, err)
	}
	second, err := e.st.ClaimTurn(ctx, "w", 30*time.Second)
	if err != nil || second.ID != t2.ID {
		t.Fatalf("want %s second, got %+v err=%v", t2.ID, second, err)
	}
	if _, err := e.st.ClaimTurn(ctx, "w", 30*time.Second); !errors.Is(err, store.ErrNoTurn) {
		t.Errorf("empty queue: want ErrNoTurn, got %v", err)
	}
}

// AFB-18: a running turn with a live lease is not reclaimable; once the lease
// expires another worker can take it over, and the previous owner is then
// locked out.
func TestClaimTurn_ExpiredLeaseReclaimable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, _ := e.claimed(t, sess, "worker-a")

	if _, err := e.st.ClaimTurn(ctx, "worker-b", 30*time.Second); !errors.Is(err, store.ErrNoTurn) {
		t.Fatalf("live lease must not be reclaimable: got %v", err)
	}

	e.expireLease(t, turn.ID)
	re, err := e.st.ClaimTurn(ctx, "worker-b", 30*time.Second)
	if err != nil {
		t.Fatalf("expired lease must be reclaimable: %v", err)
	}
	if re.ID != turn.ID || re.LeaseOwner == nil || *re.LeaseOwner != "worker-b" || re.Status != store.TurnRunning {
		t.Errorf("reclaimed turn wrong: %+v", re)
	}
	if err := e.st.RenewLease(ctx, store.Lease{TurnID: turn.ID, Owner: "worker-a"}, time.Minute); !errors.Is(err, store.ErrLeaseLost) {
		t.Errorf("old owner RenewLease: want ErrLeaseLost, got %v", err)
	}
}

// AFB-18: every write by a stale lease holder is rejected with ErrLeaseLost
// and has no effect.
func TestLeaseChecked_StaleLeaseWritesRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, stale := e.claimed(t, sess, "worker-a")

	e.expireLease(t, turn.ID)
	if _, err := e.st.ClaimTurn(ctx, "worker-b", 30*time.Second); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	fresh := store.Lease{TurnID: turn.ID, Owner: "worker-b"}

	if _, err := e.st.AppendMessage(ctx, stale, store.NewMessage{Role: store.RoleAssistant, Content: "stale"}); !errors.Is(err, store.ErrLeaseLost) {
		t.Errorf("AppendMessage: want ErrLeaseLost, got %v", err)
	}
	if _, err := e.st.AppendEvent(ctx, stale, "tool_call", nil); !errors.Is(err, store.ErrLeaseLost) {
		t.Errorf("AppendEvent: want ErrLeaseLost, got %v", err)
	}
	if err := e.st.SaveSnapshot(ctx, stale, "sha", []byte("bundle")); !errors.Is(err, store.ErrLeaseLost) {
		t.Errorf("SaveSnapshot: want ErrLeaseLost, got %v", err)
	}
	if err := e.st.RenewLease(ctx, stale, time.Minute); !errors.Is(err, store.ErrLeaseLost) {
		t.Errorf("RenewLease: want ErrLeaseLost, got %v", err)
	}
	if err := e.st.FinishTurn(ctx, stale, store.FinishParams{Status: store.TurnCompleted}); !errors.Is(err, store.ErrLeaseLost) {
		t.Errorf("FinishTurn: want ErrLeaseLost, got %v", err)
	}

	// No stale write left a trace; the new owner still works.
	msgs, _ := e.st.ListMessages(ctx, sess.ID)
	evs, _ := e.st.ListEvents(ctx, e.userA, sess.ID, 0, 50)
	snaps, _ := e.st.ListSnapshots(ctx, sess.ID)
	if len(msgs) != 1 || len(evs) != 0 || len(snaps) != 0 { // 1 = the user message
		t.Errorf("stale writes leaked: %d messages, %d events, %d snapshots", len(msgs), len(evs), len(snaps))
	}
	if _, err := e.st.AppendMessage(ctx, fresh, store.NewMessage{Role: store.RoleAssistant, Content: "ok"}); err != nil {
		t.Errorf("new owner AppendMessage: %v", err)
	}
	// Once finished, even the owner cannot write any more.
	if err := e.st.FinishTurn(ctx, fresh, store.FinishParams{Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AppendEvent(ctx, fresh, "late", nil); !errors.Is(err, store.ErrLeaseLost) {
		t.Errorf("write after finish: want ErrLeaseLost, got %v", err)
	}
}

// RenewLease pushes lease_until forward for the owner.
func TestRenewLease_ExtendsLease(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, lease := e.claimed(t, sess, "w1")

	if err := e.st.RenewLease(ctx, lease, 10*time.Minute); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	got, err := e.st.GetTurn(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LeaseUntil == nil || !got.LeaseUntil.After(turn.LeaseUntil.Add(5*time.Minute)) {
		t.Errorf("lease not extended: before=%v after=%v", turn.LeaseUntil, got.LeaseUntil)
	}
}

// AFB-06: events are returned in id order, strictly after the cursor, capped
// by limit; an empty page for the owner is not an error.
func TestListEvents_CursorOrdering(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, lease := e.claimed(t, sess, "w1")

	var ids []int64
	for i := 0; i < 5; i++ {
		id, err := e.st.AppendEvent(ctx, lease, "tool_call", map[string]any{"name": "read_file", "n": i})
		if err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		ids = append(ids, id)
	}
	// An unleased session event interleaves in the same cursor space.
	sid, err := e.st.AppendSessionEvent(ctx, sess.ID, turn.ID, "proposal_confirmed", nil)
	if err != nil {
		t.Fatalf("AppendSessionEvent: %v", err)
	}
	ids = append(ids, sid)

	all, err := e.st.ListEvents(ctx, e.userA, sess.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 {
		t.Fatalf("want 6 events, got %d", len(all))
	}
	for i := range all {
		if all[i].ID != ids[i] {
			t.Errorf("event %d out of order: want id %d got %d", i, ids[i], all[i].ID)
		}
		if i > 0 && all[i].ID <= all[i-1].ID {
			t.Errorf("ids not strictly increasing")
		}
	}

	after, err := e.st.ListEvents(ctx, e.userA, sess.ID, ids[2], 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 3 || after[0].ID != ids[3] {
		t.Errorf("after cursor %d: want 3 events starting at %d, got %+v", ids[2], ids[3], after)
	}
	var payload map[string]any
	if err := json.Unmarshal(all[0].Payload, &payload); err != nil || payload["name"] != "read_file" {
		t.Errorf("payload not round-tripped: %s (%v)", all[0].Payload, err)
	}
	if string(all[5].Payload) != "{}" {
		t.Errorf("nil payload must be {}, got %s", all[5].Payload)
	}

	page, err := e.st.ListEvents(ctx, e.userA, sess.ID, 0, 2)
	if err != nil || len(page) != 2 || page[1].ID != ids[1] {
		t.Errorf("limit not applied: %+v err=%v", page, err)
	}
	empty, err := e.st.ListEvents(ctx, e.userA, sess.ID, ids[5], 100)
	if err != nil || len(empty) != 0 {
		t.Errorf("past the last event: want empty and no error, got %+v err=%v", empty, err)
	}
}

// Events of one session never appear in another session's feed, even for the
// same owner.
func TestListEvents_ScopedToSession(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s1 := e.session(t, e.userA, e.appA)
	s2 := e.session(t, e.userA, e.appA)
	if _, err := e.st.AppendSessionEvent(ctx, s1.ID, "", "revert", nil); err != nil {
		t.Fatal(err)
	}
	got, err := e.st.ListEvents(ctx, e.userA, s2.ID, 0, 10)
	if err != nil || len(got) != 0 {
		t.Errorf("s2 feed must be empty, got %+v err=%v", got, err)
	}
	if _, err := e.st.AppendSessionEvent(ctx, "00000000-0000-0000-0000-000000000000", "", "x", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("event on unknown session: want ErrNotFound, got %v", err)
	}
}

// MarkQueued flips pending->queued once and emits exactly one 'queued' event.
func TestMarkQueued_OnceWithSingleEvent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "go")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, false, false} {
		changed, err := e.st.MarkQueued(ctx, turn.ID)
		if err != nil || changed != want {
			t.Fatalf("call %d: changed=%v err=%v, want changed=%v", i, changed, err, want)
		}
	}
	evs, _ := e.st.ListEvents(ctx, e.userA, sess.ID, 0, 10)
	if len(evs) != 1 || evs[0].Type != "queued" {
		t.Errorf("want exactly one queued event, got %+v", evs)
	}
	if s, _ := e.st.GetSession(ctx, e.userA, sess.ID); s.Status != store.SessionQueued {
		t.Errorf("session status want queued, got %s", s.Status)
	}
}

// FinishTurn: terminal status clears the lease, stamps finished_at, advances
// the session head and sets the session status; failure keeps only the
// generic error code.
func TestFinishTurn_UpdatesTurnAndSession(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, lease := e.claimed(t, sess, "w1")

	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnCompleted, CommitSHA: "abc123"}); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	got, _ := e.st.GetTurn(ctx, turn.ID)
	if got.Status != store.TurnCompleted || got.CommitSHA != "abc123" || got.LeaseOwner != nil || got.LeaseUntil != nil || got.FinishedAt == nil {
		t.Errorf("turn not finished cleanly: %+v", got)
	}
	s, _ := e.st.GetSession(ctx, e.userA, sess.ID)
	if s.Status != store.SessionIdle || s.HeadSHA != "abc123" || s.HeadTurnSeq != turn.Seq {
		t.Errorf("session head/status wrong: %+v", s)
	}
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnCompleted}); !errors.Is(err, store.ErrLeaseLost) {
		t.Errorf("double finish: want ErrLeaseLost, got %v", err)
	}

	// failed: generic code, no commit -> head untouched, session failed.
	t2, lease2 := e.claimed(t, s, "w1")
	if err := e.st.FinishTurn(ctx, lease2, store.FinishParams{Status: store.TurnFailed, ErrorCode: "provider_unavailable"}); err != nil {
		t.Fatal(err)
	}
	f, _ := e.st.GetTurn(ctx, t2.ID)
	if f.ErrorCode == nil || *f.ErrorCode != "provider_unavailable" {
		t.Errorf("error code not stored: %+v", f)
	}
	s, _ = e.st.GetSession(ctx, e.userA, sess.ID)
	if s.Status != store.SessionFailed || s.HeadSHA != "abc123" {
		t.Errorf("failed turn must keep head and mark session failed: %+v", s)
	}

	if err := e.st.FinishTurn(ctx, lease2, store.FinishParams{Status: "bogus"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("unknown status: want ErrInvalidArgument, got %v", err)
	}
}

// A turn paused for confirmation keeps its proposal and is not claimable.
func TestFinishTurn_AwaitingConfirmationNotClaimable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, lease := e.claimed(t, sess, "w1")

	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnAwaitingConfirmation}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("pause without proposal: want ErrInvalidArgument, got %v", err)
	}
	proposal := json.RawMessage(`{"tool_call_id":"call_1","operation":{"type":"create_table"}}`)
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnAwaitingConfirmation, PendingProposal: proposal}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, _ := e.st.GetTurn(ctx, turn.ID)
	var back map[string]any
	if err := json.Unmarshal(got.PendingProposal, &back); err != nil || back["tool_call_id"] != "call_1" {
		t.Errorf("proposal not stored: %s (%v)", got.PendingProposal, err)
	}
	if got.Status != store.TurnAwaitingConfirmation || got.FinishedAt != nil || got.LeaseOwner != nil {
		t.Errorf("paused turn state wrong: %+v", got)
	}
	if s, _ := e.st.GetSession(ctx, e.userA, sess.ID); s.Status != store.SessionAwaitingConfirmation {
		t.Errorf("session status want awaiting_confirmation, got %s", s.Status)
	}
	if _, err := e.st.ClaimTurn(ctx, "w2", 30*time.Second); !errors.Is(err, store.ErrNoTurn) {
		t.Errorf("awaiting_confirmation turn must not be claimable, got %v", err)
	}
}

// Messages keep insertion order and round-trip tool calls / tool_call_id.
func TestAppendMessage_RoundTripAndOrder(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	_, lease := e.claimed(t, sess, "w1")

	calls := json.RawMessage(`[{"id":"call_1","name":"read_file","arguments":"{\"path\":\"src/App.tsx\"}"}]`)
	if _, err := e.st.AppendMessage(ctx, lease, store.NewMessage{Role: store.RoleAssistant, Content: "reading", ToolCalls: calls}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AppendMessage(ctx, lease, store.NewMessage{Role: store.RoleTool, Content: "file body", ToolCallID: "call_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AppendMessage(ctx, lease, store.NewMessage{Role: "robot"}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("unknown role: want ErrInvalidArgument, got %v", err)
	}

	msgs, err := e.st.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 || msgs[0].Role != store.RoleUser || msgs[1].Role != store.RoleAssistant || msgs[2].Role != store.RoleTool {
		t.Fatalf("unexpected order/roles: %+v", msgs)
	}
	var c []map[string]any
	if err := json.Unmarshal(msgs[1].ToolCalls, &c); err != nil || c[0]["id"] != "call_1" {
		t.Errorf("tool calls not round-tripped: %s (%v)", msgs[1].ToolCalls, err)
	}
	if msgs[2].ToolCallID != "call_1" || msgs[0].ToolCallID != "" || msgs[0].ToolCalls != nil {
		t.Errorf("tool_call_id/tool_calls wrong: %+v", msgs)
	}
}

// Snapshots: turn 0 / part 0 (owner-scoped, sets head) plus lease-checked
// per-turn parts, listed in (turn_seq, part) order. Each save of a turn is its
// own row.
func TestSnapshots_SaveAndList(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)

	if err := e.st.SaveInitialSnapshot(ctx, e.userB, sess.ID, "sha0", []byte("full")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign SaveInitialSnapshot: want ErrNotFound, got %v", err)
	}
	if err := e.st.SaveInitialSnapshot(ctx, e.userA, sess.ID, "sha0", []byte("full")); err != nil {
		t.Fatalf("SaveInitialSnapshot: %v", err)
	}
	// Idempotent: a retried session creation replaces turn 0 / part 0.
	if err := e.st.SaveInitialSnapshot(ctx, e.userA, sess.ID, "sha0", []byte("full")); err != nil {
		t.Fatalf("SaveInitialSnapshot (retry): %v", err)
	}
	turn, lease := e.claimed(t, sess, "w1")
	if err := e.st.SaveSnapshot(ctx, lease, "sha1", []byte("inc-1")); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if err := e.st.SaveSnapshot(ctx, lease, "sha1b", []byte("inc-1b")); err != nil {
		t.Fatalf("SaveSnapshot (second batch): %v", err)
	}

	snaps, err := e.st.ListSnapshots(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 3 {
		t.Fatalf("want 3 snapshots (turn 0 + 2 parts), got %d", len(snaps))
	}
	if snaps[0].TurnSeq != 0 || snaps[0].Part != 0 || snaps[0].CommitSHA != "sha0" || string(snaps[0].Bundle) != "full" || snaps[0].SizeBytes != 4 {
		t.Errorf("turn-0 snapshot wrong: %+v", snaps[0])
	}
	if snaps[1].TurnSeq != turn.Seq || snaps[1].Part != 0 || snaps[1].CommitSHA != "sha1" || string(snaps[1].Bundle) != "inc-1" || snaps[1].SizeBytes != 5 {
		t.Errorf("first part wrong: %+v", snaps[1])
	}
	if snaps[2].TurnSeq != turn.Seq || snaps[2].Part != 1 || snaps[2].CommitSHA != "sha1b" || string(snaps[2].Bundle) != "inc-1b" || snaps[2].SizeBytes != 6 {
		t.Errorf("second part wrong or replaced the first: %+v", snaps[2])
	}
	if s, _ := e.st.GetSession(ctx, e.userA, sess.ID); s.HeadSHA != "sha0" || s.HeadTurnSeq != 0 {
		t.Errorf("initial snapshot must set the session head: %+v", s)
	}
	if err := e.st.SaveSnapshot(ctx, lease, "", nil); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("empty bundle: want ErrInvalidArgument, got %v", err)
	}
}

// Several turns, several parts each: ListSnapshots orders by (turn_seq, part),
// and parts restart at 0 for every turn.
func TestSnapshots_MultiplePartsPerTurnOrdered(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	if err := e.st.SaveInitialSnapshot(ctx, e.userA, sess.ID, "sha0", []byte("full")); err != nil {
		t.Fatal(err)
	}

	var want []string
	want = append(want, "0/0/sha0")
	for turnN := 1; turnN <= 2; turnN++ {
		turn, lease := e.claimed(t, sess, "w1")
		for part := 0; part < 4; part++ {
			sha := fmt.Sprintf("sha-%d-%d", turnN, part)
			if err := e.st.SaveSnapshot(ctx, lease, sha, []byte("b-"+sha)); err != nil {
				t.Fatalf("SaveSnapshot %s: %v", sha, err)
			}
			want = append(want, fmt.Sprintf("%d/%d/%s", turn.Seq, part, sha))
		}
		if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
	}

	snaps, err := e.st.ListSnapshots(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, sn := range snaps {
		got = append(got, fmt.Sprintf("%d/%d/%s", sn.TurnSeq, sn.Part, sn.CommitSHA))
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("snapshots (turn/part/sha)\n got %v\nwant %v", got, want)
	}
}

// Concurrent saves of the same leased turn must each get a distinct part with
// no gaps and no primary-key failure (the part is assigned under a row lock).
func TestSnapshots_PartAssignedAtomicallyUnderConcurrentSaves(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, lease := e.claimed(t, sess, "w1")

	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- e.st.SaveSnapshot(ctx, lease, fmt.Sprintf("sha-%d", i), []byte(fmt.Sprintf("b-%d", i)))
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent SaveSnapshot: %v", err)
		}
	}

	snaps, err := e.st.ListSnapshots(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != n {
		t.Fatalf("want %d snapshots, got %d", n, len(snaps))
	}
	seen := map[string]bool{}
	for i, sn := range snaps {
		if sn.TurnSeq != turn.Seq || sn.Part != i {
			t.Errorf("row %d: turn_seq=%d part=%d, want turn_seq=%d part=%d (parts must be 0..%d without gaps)", i, sn.TurnSeq, sn.Part, turn.Seq, i, n-1)
		}
		seen[sn.CommitSHA] = true
	}
	if len(seen) != n {
		t.Errorf("a save was lost or duplicated: %d distinct shas, want %d", len(seen), n)
	}
}

// A stale lease holder cannot append a part, and leaves no row behind.
func TestSnapshots_StaleLeaseRejectedNoRow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	turn, stale := e.claimed(t, sess, "worker-a")
	if err := e.st.SaveSnapshot(ctx, stale, "sha1", []byte("inc-1")); err != nil {
		t.Fatal(err)
	}

	e.expireLease(t, turn.ID)
	if _, err := e.st.ClaimTurn(ctx, "worker-b", 30*time.Second); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if err := e.st.SaveSnapshot(ctx, stale, "sha-stale", []byte("x")); !errors.Is(err, store.ErrLeaseLost) {
		t.Fatalf("stale SaveSnapshot: want ErrLeaseLost, got %v", err)
	}
	fresh := store.Lease{TurnID: turn.ID, Owner: "worker-b"}
	if err := e.st.SaveSnapshot(ctx, fresh, "sha2", []byte("inc-2")); err != nil {
		t.Fatalf("new owner SaveSnapshot: %v", err)
	}

	snaps, _ := e.st.ListSnapshots(ctx, sess.ID)
	if len(snaps) != 2 || snaps[0].CommitSHA != "sha1" || snaps[1].CommitSHA != "sha2" || snaps[1].Part != 1 {
		t.Errorf("stale save leaked or disturbed the parts: %+v", snaps)
	}
}

// Archiving a session stops the worker from claiming its pending turn.
func TestClaimTurn_SkipsArchivedSessions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "go"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ArchiveSession(ctx, e.userA, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ClaimTurn(ctx, "w1", 30*time.Second); !errors.Is(err, store.ErrNoTurn) {
		t.Errorf("archived session turn must not be claimed, got %v", err)
	}
}

// Deleting the linked backend app cascades the whole session tree (design:
// "Linked backend app deleted").
func TestCascade_BackendAppDeleteRemovesSessionTree(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	_, lease := e.claimed(t, sess, "w1")
	if _, err := e.st.AppendEvent(ctx, lease, "turn_started", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveSnapshot(ctx, lease, "sha", []byte("b")); err != nil {
		t.Fatal(err)
	}

	if _, err := e.pool.Exec(ctx, `DELETE FROM zeep_system.apps WHERE id = $1`, e.appA); err != nil {
		t.Fatalf("delete app: %v", err)
	}
	for _, table := range []string{"builder_sessions", "builder_turns", "builder_messages", "builder_events", "builder_snapshots"} {
		var n int
		if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM zeep_system.`+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: %d rows left after app delete", table, n)
		}
	}
}

// Input validation on CreateSession.
func TestCreateSession_Validation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	base := store.CreateSessionParams{OwnerUserID: e.userA, BackendAppID: e.appA, TemplateKind: store.TemplateDefault, TemplateVersion: "v0"}

	bad := base
	bad.TemplateKind = "other"
	if _, err := e.st.CreateSession(ctx, bad); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("bad template kind: want ErrInvalidArgument, got %v", err)
	}
	bad = base
	bad.TemplateVersion = ""
	if _, err := e.st.CreateSession(ctx, bad); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("missing template version: want ErrInvalidArgument, got %v", err)
	}
	if _, err := e.st.CreateTurn(ctx, e.userA, "00000000-0000-0000-0000-000000000000", "bogus", "x"); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("bad turn kind: want ErrInvalidArgument, got %v", err)
	}
}
