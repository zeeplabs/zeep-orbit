package store_test

// attach_test.go: the attach_backend turn (builder-no-backend NB-08, T11c).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

var newAttach = store.AttachPayload{Mode: store.AttachModeNew, AppName: "shop-ab12", Config: "export default {}\n"}

func (e *env) noBackendSession(t *testing.T, phase string) *store.Session {
	t.Helper()
	s, err := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: e.userA, Title: "t", TemplateKind: store.TemplateDefault, TemplateVersion: "1",
		BackendOrigin: store.BackendOriginNone, Phase: phase,
	})
	if err != nil {
		t.Fatalf("CreateSession (none): %v", err)
	}
	return s
}

func TestCreateAttachTurn_PayloadRoundTripsAndFinishClearsIt(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.noBackendSession(t, "")

	turn, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach)
	if err != nil {
		t.Fatalf("CreateAttachTurn: %v", err)
	}
	if turn.Kind != store.KindAttachBackend || turn.Status != store.TurnPending {
		t.Errorf("turn = %+v, want a pending attach turn", turn)
	}
	back, err := e.st.GetTurn(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := store.AttachPayloadOf(back); !ok || got != newAttach {
		t.Errorf("payload = %+v ok=%v, want %+v", got, ok, newAttach)
	}
	if _, ok := store.RevertTargetOf(back); ok {
		t.Error("an attach turn must not read as a revert")
	}
	claimed, err := e.st.ClaimTurn(ctx, "w", time.Minute)
	if err != nil || claimed.ID != turn.ID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if err := e.st.FinishTurn(ctx, store.Lease{TurnID: turn.ID, Owner: "w"}, store.FinishParams{Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	done, _ := e.st.GetTurn(ctx, turn.ID)
	if _, ok := store.AttachPayloadOf(done); ok {
		t.Error("finishing the turn must clear the payload")
	}
}

func TestCreateAttachTurn_Rejections(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.noBackendSession(t, "")

	for name, p := range map[string]store.AttachPayload{
		"no config":           {Mode: store.AttachModeNew, AppName: "a"},
		"new without a name":  {Mode: store.AttachModeNew, Config: "x"},
		"new with an app id":  {Mode: store.AttachModeNew, AppName: "a", AppID: "x", Config: "x"},
		"existing without id": {Mode: store.AttachModeExisting, Config: "x"},
		"unknown mode":        {Mode: "none", Config: "x"},
	} {
		if _, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, p); !errors.Is(err, store.ErrInvalidArgument) {
			t.Errorf("%s: want ErrInvalidArgument, got %v", name, err)
		}
	}
	if _, err := e.st.CreateAttachTurn(ctx, e.userB, sess.ID, newAttach); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("foreign user: want ErrNotFound, got %v", err)
	}
	withBackend := e.session(t, e.userA, e.appA)
	if _, err := e.st.CreateAttachTurn(ctx, e.userA, withBackend.ID, newAttach); !errors.Is(err, store.ErrBackendAlreadyAttached) {
		t.Errorf("session with a backend: want ErrBackendAlreadyAttached, got %v", err)
	}

	// A chat turn in progress (not a paused card) blocks the attach and is left alone.
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "hi"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach); !errors.Is(err, store.ErrTurnActive) {
		t.Errorf("chat turn pending: want ErrTurnActive, got %v", err)
	}
	e.complete(t, store.TurnCompleted)

	if _, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach); !errors.Is(err, store.ErrTurnActive) {
		t.Errorf("second attach: want ErrTurnActive, got %v", err)
	}
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "hi"); !errors.Is(err, store.ErrTurnActive) {
		t.Errorf("chat turn during an attach: want ErrTurnActive, got %v", err)
	}

	if err := e.st.ArchiveSession(ctx, e.userA, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach); !errors.Is(err, store.ErrSessionArchived) {
		t.Errorf("archived: want ErrSessionArchived, got %v", err)
	}
}

func TestCreateAttachTurn_EndsAPausedPlanOrQuestionCard(t *testing.T) {
	for _, tc := range []struct{ tool, args string }{
		{"propose_build_plan", planArgs},
		{"ask_user", questionArgs},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()
			sess := e.noBackendSession(t, store.PhaseBriefing)
			paused := e.pausedOn(t, sess, store.TurnOptions{}, tc.tool, tc.args)

			turn, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach)
			if err != nil {
				t.Fatalf("CreateAttachTurn over a paused %s: %v", tc.tool, err)
			}
			if turn.Kind != store.KindAttachBackend || turn.Status != store.TurnPending {
				t.Errorf("turn = %+v", turn)
			}
			old, err := e.st.GetTurn(ctx, paused.ID)
			if err != nil || old.Status != store.TurnCompleted || len(old.PendingProposal) != 0 {
				t.Errorf("paused turn = %+v, %v; want completed with no proposal", old, err)
			}
			// The card's tool call got an answer, so the conversation stays valid.
			if m := e.lastToolMessage(t, sess.ID); m.ToolCallID != "call_1" || m.Content == "" {
				t.Errorf("tool message = %+v, want an answer to call_1", m)
			}
			var n int
			if err := e.pool.QueryRow(ctx,
				`SELECT count(*) FROM zeep_system.builder_events WHERE turn_id = $1 AND type = $2 AND payload->>'reason' = 'backend_added'`,
				paused.ID, store.EventPlanDismissed).Scan(&n); err != nil || n != 1 {
				t.Errorf("plan_dismissed events = %d, %v; want 1", n, err)
			}
			got, _ := e.st.GetSessionByID(ctx, sess.ID)
			if got.Status != store.SessionIdle || got.Phase != store.PhaseBriefing {
				t.Errorf("session status %q phase %q; want idle and still briefing (nothing approved)", got.Status, got.Phase)
			}
		})
	}
}

func TestCreateAttachTurn_LeavesAnyOtherPausedTurnAlone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.noBackendSession(t, "")
	paused := e.pausedOn(t, sess, store.TurnOptions{}, "propose_backend_change", `{}`)
	if _, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach); !errors.Is(err, store.ErrTurnActive) {
		t.Fatalf("want ErrTurnActive, got %v", err)
	}
	if got, _ := e.st.GetTurn(ctx, paused.ID); got.Status != store.TurnAwaitingConfirmation || len(got.PendingProposal) == 0 {
		t.Errorf("the paused turn changed: %+v", got)
	}
}

func TestAttachBackend_ConditionalAndLinksTheFrontendApp(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.noBackendSession(t, "")

	var tplID, faID string
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates (name, github_owner, github_repo, created_by) VALUES ('t','o','r','x') RETURNING id`).Scan(&tplID); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (name, slug, template_id, created_by) VALUES ('n','att-slug',$1,'x') RETURNING id`, tplID).Scan(&faID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM zeep_system.builder_sessions WHERE id = $1`, sess.ID)
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM zeep_system.frontend_apps WHERE id = $1`, faID)
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM zeep_system.github_templates WHERE id = $1`, tplID)
	})
	if err := e.st.RecordPublish(ctx, e.userA, sess.ID, &faID, "1111111111111111111111111111111111111111"); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []struct{ app, origin string }{{"", store.BackendOriginBuilder}, {e.appA, "none"}, {e.appA, ""}} {
		if err := e.st.AttachBackend(ctx, sess.ID, bad.app, bad.origin); !errors.Is(err, store.ErrInvalidArgument) {
			t.Errorf("AttachBackend(%q, %q): want ErrInvalidArgument, got %v", bad.app, bad.origin, err)
		}
	}
	if err := e.st.AttachBackend(ctx, "00000000-0000-0000-0000-000000000000", e.appA, store.BackendOriginBuilder); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown session: want ErrNotFound, got %v", err)
	}
	if err := e.st.AttachBackend(ctx, sess.ID, e.appA, store.BackendOriginBuilder); err != nil {
		t.Fatalf("AttachBackend: %v", err)
	}
	got, _ := e.st.GetSessionByID(ctx, sess.ID)
	if got.BackendAppID != e.appA || got.BackendOrigin != store.BackendOriginBuilder {
		t.Errorf("session = app %q origin %q", got.BackendAppID, got.BackendOrigin)
	}
	var link string
	if err := e.pool.QueryRow(ctx, `SELECT COALESCE(backend_app_id::text,'') FROM zeep_system.frontend_apps WHERE id = $1`, faID).Scan(&link); err != nil || link != e.appA {
		t.Errorf("frontend app link = %q, %v; want %s", link, err, e.appA)
	}
	// The second attach loses, and does not move the link.
	if err := e.st.AttachBackend(ctx, sess.ID, e.appB, store.BackendOriginExisting); !errors.Is(err, store.ErrBackendAlreadyAttached) {
		t.Errorf("second attach: want ErrBackendAlreadyAttached, got %v", err)
	}
	if got2, _ := e.st.GetSessionByID(ctx, sess.ID); got2.BackendAppID != e.appA {
		t.Errorf("the losing attach changed the session: %q", got2.BackendAppID)
	}
}

func TestAttachBackend_ConcurrentAttachesHaveOneWinner(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.noBackendSession(t, "")
	var wg sync.WaitGroup
	results := make([]error, 6)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			app := e.appA
			if i%2 == 1 {
				app = e.appB
			}
			results[i] = e.st.AttachBackend(ctx, sess.ID, app, store.BackendOriginExisting)
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, store.ErrBackendAlreadyAttached):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d attaches won, want exactly 1", wins)
	}
}

func TestCreateRevertTurn_RefusesToCrossACompletedAttach(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.noBackendSession(t, "")
	if err := e.st.SaveInitialSnapshot(ctx, e.userA, sess.ID, "sha0", []byte("b0")); err != nil {
		t.Fatal(err)
	}
	e.finishedTurn(t, sess, "sha1") // turn 1: a chat turn

	// A failed attach (turn 2) changes nothing: reverting across it stays possible.
	if _, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach); err != nil {
		t.Fatal(err)
	}
	e.complete(t, store.TurnFailed)
	if _, err := e.st.CreateRevertTurn(ctx, e.userA, sess.ID, 1); err != nil {
		t.Fatalf("revert across a failed attach: %v", err)
	}
	e.complete(t, store.TurnCompleted) // turn 3: the revert

	// A completed attach (turn 4) is a barrier for every target before it.
	if _, err := e.st.CreateAttachTurn(ctx, e.userA, sess.ID, newAttach); err != nil {
		t.Fatal(err)
	}
	turn := e.complete(t, store.TurnCompleted)
	for seq := 0; seq < turn.Seq; seq++ {
		if _, err := e.st.CreateRevertTurn(ctx, e.userA, sess.ID, seq); !errors.Is(err, store.ErrRevertAcrossBackend) {
			t.Errorf("revert to %d: want ErrRevertAcrossBackend, got %v", seq, err)
		}
	}
	// The attach turn itself, and anything after it, is a valid target.
	if _, err := e.st.CreateRevertTurn(ctx, e.userA, sess.ID, turn.Seq); err != nil {
		t.Errorf("revert to the attach turn: %v", err)
	}
}
