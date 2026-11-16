package store_test

// revert_test.go — CreateRevertTurn / RevertTargetOf (ai-frontend-builder T28).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// finished runs a chat turn to completion, saving one snapshot per sha.
func (e *env) finishedTurn(t *testing.T, sess *store.Session, shas ...string) *store.Turn {
	t.Helper()
	ctx := context.Background()
	turn, lease := e.claimed(t, sess, "w")
	for _, sha := range shas {
		if err := e.st.SaveSnapshot(ctx, lease, sha, []byte("b-"+sha)); err != nil {
			t.Fatalf("SaveSnapshot: %v", err)
		}
	}
	last := ""
	if len(shas) > 0 {
		last = shas[len(shas)-1]
	}
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnCompleted, CommitSHA: last}); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	return turn
}

func TestCreateRevertTurn_ResolvesTheStateAfterTheTurn(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	if err := e.st.SaveInitialSnapshot(ctx, e.userA, sess.ID, "sha0", []byte("b0")); err != nil {
		t.Fatal(err)
	}
	e.finishedTurn(t, sess, "sha1a", "sha1b") // turn 1 committed twice: the head after it is sha1b
	e.finishedTurn(t, sess)                   // turn 2 committed nothing

	for seq, want := range map[int]string{0: "sha0", 1: "sha1b", 2: "sha1b"} {
		turn, err := e.st.CreateRevertTurn(ctx, e.userA, sess.ID, seq)
		if err != nil {
			t.Fatalf("revert to %d: %v", seq, err)
		}
		if turn.Kind != store.KindRevert || turn.Status != store.TurnPending {
			t.Errorf("turn = %+v, want a pending revert turn", turn)
		}
		target, ok := store.RevertTargetOf(turn)
		if !ok || target.TurnSeq != seq || target.SHA != want {
			t.Errorf("revert to %d: target = %+v ok=%v, want %s", seq, target, ok, want)
		}
		// The target survives a round trip through the database.
		back, err := e.st.GetTurn(ctx, turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		if again, ok := store.RevertTargetOf(back); !ok || again != target {
			t.Errorf("stored target = %+v ok=%v, want %+v", again, ok, target)
		}
		// Finishing the turn clears it.
		claimed, err := e.st.ClaimTurn(ctx, "w", time.Minute)
		if err != nil || claimed.ID != turn.ID {
			t.Fatalf("claim: %+v %v", claimed, err)
		}
		if err := e.st.FinishTurn(ctx, store.Lease{TurnID: turn.ID, Owner: "w"}, store.FinishParams{Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCreateRevertTurn_Rejections(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	if err := e.st.SaveInitialSnapshot(ctx, e.userA, sess.ID, "sha0", []byte("b0")); err != nil {
		t.Fatal(err)
	}
	e.finishedTurn(t, sess, "sha1")

	for _, seq := range []int{-1, 2, 99} {
		if _, err := e.st.CreateRevertTurn(ctx, e.userA, sess.ID, seq); !errors.Is(err, store.ErrNoRevertTarget) {
			t.Errorf("turn %d: want ErrNoRevertTarget, got %v", seq, err)
		}
	}
	if _, err := e.st.CreateRevertTurn(ctx, e.userB, sess.ID, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign user: want ErrNotFound, got %v", err)
	}
	// A session without any snapshot has nothing to restore from.
	bare := e.session(t, e.userA, e.appA)
	e.finishedTurn(t, bare)
	if _, err := e.st.CreateRevertTurn(ctx, e.userA, bare.ID, 1); !errors.Is(err, store.ErrNoRevertTarget) {
		t.Errorf("no snapshots: want ErrNoRevertTarget, got %v", err)
	}

	if _, err := e.st.CreateRevertTurn(ctx, e.userA, sess.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateRevertTurn(ctx, e.userA, sess.ID, 1); !errors.Is(err, store.ErrTurnActive) {
		t.Errorf("second revert: want ErrTurnActive, got %v", err)
	}
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "hi"); !errors.Is(err, store.ErrTurnActive) {
		t.Errorf("chat turn during a revert: want ErrTurnActive, got %v", err)
	}

	if err := e.st.ArchiveSession(ctx, e.userA, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateRevertTurn(ctx, e.userA, sess.ID, 1); !errors.Is(err, store.ErrSessionArchived) {
		t.Errorf("archived: want ErrSessionArchived, got %v", err)
	}
}

func TestCreateRevertTurn_ConcurrentRequestsCreateOneTurn(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	if err := e.st.SaveInitialSnapshot(ctx, e.userA, sess.ID, "sha0", []byte("b0")); err != nil {
		t.Fatal(err)
	}
	e.finishedTurn(t, sess, "sha1")

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.st.CreateRevertTurn(ctx, e.userA, sess.ID, 1)
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, store.ErrTurnActive):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d requests created a turn, want exactly 1", ok)
	}
}

func TestRevertTargetOf_OnlyRevertTurnsWithATarget(t *testing.T) {
	if _, ok := store.RevertTargetOf(nil); ok {
		t.Error("nil turn")
	}
	if _, ok := store.RevertTargetOf(&store.Turn{Kind: store.KindChat, PendingProposal: []byte(`{"revert":{"turn_seq":1,"sha":"x"}}`)}); ok {
		t.Error("a chat turn has no revert target")
	}
	for _, raw := range []string{``, `{}`, `{"revert":{"turn_seq":1}}`, `{"revert":{"turn_seq":-1,"sha":"x"}}`, `nope`} {
		if _, ok := store.RevertTargetOf(&store.Turn{Kind: store.KindRevert, PendingProposal: []byte(raw)}); ok {
			t.Errorf("payload %q must not yield a target", raw)
		}
	}
}
