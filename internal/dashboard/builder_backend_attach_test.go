package dashboard

// builder_backend_attach_test.go — the attach_backend callback (NB-08, T11d-2)
// against a real database: app creation, the conditional session update, the
// idempotent retry and the compensation.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// noBackendSession creates a session with no backend owned by owner.
func (e *bbEnv) noBackendSession(t *testing.T, owner *DashboardUser) *store.Session {
	t.Helper()
	s, err := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: owner.ID, TemplateKind: store.TemplateDefault, TemplateVersion: "1",
		BackendOrigin: store.BackendOriginNone,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return s
}

func (e *bbEnv) reload(t *testing.T, s *store.Session) *store.Session {
	t.Helper()
	got, err := e.st.GetSession(context.Background(), s.OwnerUserID, s.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	return got
}

func TestAttachBackend_NewCreatesTheAppAndLinksTheSession(t *testing.T) {
	e := newBBEnv(t)
	s := e.noBackendSession(t, e.owner)
	before := e.appCount(t)

	id, err := e.h.attachBackend(context.Background(), s, store.AttachPayload{
		Mode: store.AttachModeNew, AppName: "loja-abc123", Config: "x",
	})
	if err != nil {
		t.Fatalf("attachBackend: %v", err)
	}
	if n := e.appCount(t); n != before+1 {
		t.Errorf("apps %d -> %d, want one more", before, n)
	}
	got := e.reload(t, s)
	if got.BackendAppID != id || got.BackendOrigin != store.BackendOriginBuilder {
		t.Errorf("session backend = %q/%q, want %q/%q", got.BackendAppID, got.BackendOrigin, id, store.BackendOriginBuilder)
	}
	if n := e.auditCount(t, "app.create"); n != 1 {
		t.Errorf("app.create audit rows = %d, want 1 (same path as POST /api/apps)", n)
	}
}

func TestAttachBackend_NewIsIdempotent(t *testing.T) {
	e := newBBEnv(t)
	s := e.noBackendSession(t, e.owner)
	p := store.AttachPayload{Mode: store.AttachModeNew, AppName: "loja-abc123", Config: "x"}

	first, err := e.h.attachBackend(context.Background(), s, p)
	if err != nil {
		t.Fatal(err)
	}
	before := e.appCount(t)
	// The claim of a resumed turn still holds the old session (no backend).
	again, err := e.h.attachBackend(context.Background(), s, p)
	if err != nil || again != first {
		t.Fatalf("retry = %q, %v; want the first app %q", again, err, first)
	}
	if n := e.appCount(t); n != before {
		t.Errorf("a retry must not create another app: %d -> %d", before, n)
	}
}

func TestAttachBackend_ExistingLinksTheApp(t *testing.T) {
	e := newBBEnv(t)
	s := e.noBackendSession(t, e.owner)
	before := e.appCount(t)

	id, err := e.h.attachBackend(context.Background(), s, store.AttachPayload{
		Mode: store.AttachModeExisting, AppID: e.appA, Config: "x",
	})
	if err != nil || id != e.appA {
		t.Fatalf("attachBackend = %q, %v", id, err)
	}
	if n := e.appCount(t); n != before {
		t.Errorf("an existing backend creates no app: %d -> %d", before, n)
	}
	if got := e.reload(t, s); got.BackendAppID != e.appA || got.BackendOrigin != store.BackendOriginExisting {
		t.Errorf("session backend = %q/%q", got.BackendAppID, got.BackendOrigin)
	}
}

func TestAttachBackend_ExistingAppTheOwnerLostAccessTo(t *testing.T) {
	e := newBBEnv(t)
	s := e.noBackendSession(t, e.owner)
	// appB belongs to someone else: the owner has no role on it.
	_, err := e.h.attachBackend(context.Background(), s, store.AttachPayload{
		Mode: store.AttachModeExisting, AppID: e.appB, Config: "x",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if got := e.reload(t, s); got.BackendAppID != "" || got.BackendOrigin != store.BackendOriginNone {
		t.Errorf("the session must stay without a backend: %q/%q", got.BackendAppID, got.BackendOrigin)
	}
}

func TestAttachBackend_AnotherBackendWonTheRace(t *testing.T) {
	e := newBBEnv(t)
	s := e.noBackendSession(t, e.owner)
	if err := e.st.AttachBackend(context.Background(), s.ID, e.appA, store.BackendOriginExisting); err != nil {
		t.Fatal(err)
	}
	before := e.appCount(t)

	// Mode existing with a different app, and mode new, both lose.
	for _, p := range []store.AttachPayload{
		{Mode: store.AttachModeExisting, AppID: e.appB, Config: "x"},
		{Mode: store.AttachModeNew, AppName: "loja-abc123", Config: "x"},
	} {
		if _, err := e.h.attachBackend(context.Background(), s, p); !errors.Is(err, store.ErrBackendAlreadyAttached) {
			t.Errorf("%s: err = %v, want ErrBackendAlreadyAttached", p.Mode, err)
		}
	}
	if n := e.appCount(t); n != before {
		t.Errorf("the loser must not leave an app: %d -> %d", before, n)
	}
	if got := e.reload(t, s); got.BackendAppID != e.appA {
		t.Errorf("the winner's backend must stay, got %q", got.BackendAppID)
	}
}

func TestAttachBackend_NewRemovesTheAppWhenTheSessionUpdateFails(t *testing.T) {
	e := newBBEnv(t)
	s := e.noBackendSession(t, e.owner)
	// The session is archived after the claim: the conditional update matches no row.
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.builder_sessions SET status = 'archived' WHERE id = $1`, s.ID); err != nil {
		t.Fatal(err)
	}
	before := e.appCount(t)

	_, err := e.h.attachBackend(context.Background(), s, store.AttachPayload{
		Mode: store.AttachModeNew, AppName: "loja-abc123", Config: "x",
	})
	if !errors.Is(err, store.ErrBackendAlreadyAttached) {
		t.Fatalf("err = %v", err)
	}
	if n := e.appCount(t); n != before {
		t.Errorf("the app created for the failed attach must be removed: %d -> %d", before, n)
	}
	if n := e.auditCount(t, "app.delete"); n != 1 {
		t.Errorf("app.delete audit rows = %d, want 1", n)
	}
}

func TestAttachBackend_WithoutDashboardFails(t *testing.T) {
	e := newBBEnv(t)
	h := NewBuilderHandler(e.pool, BuilderHandlerConfig{Store: e.st, Sandbox: e.fake})
	if _, err := h.attachBackend(context.Background(), e.noBackendSession(t, e.owner),
		store.AttachPayload{Mode: store.AttachModeNew, AppName: "a-abc123", Config: "x"}); err == nil {
		t.Fatal("want an error")
	}
}

// The handler hands its callback to the runtime when it is built.
func TestNewBuilderHandler_HandsTheAttachCallbackToTheRuntime(t *testing.T) {
	e := newBBEnv(t)
	var got agent.AttachFunc
	NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.fake, Dashboard: e.dash,
		SetAttachBackend: func(fn agent.AttachFunc) { got = fn },
	})
	if got == nil {
		t.Fatal("SetAttachBackend was not called")
	}
}

func TestBuilderRuntime_PrepareDepsCarriesTheAttachCallback(t *testing.T) {
	e := newBBEnv(t)
	rt := &BuilderRuntime{pool: e.pool, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	called := false
	rt.SetAttachBackend(func(context.Context, *store.Session, store.AttachPayload) (string, error) {
		called = true
		return "app", nil
	})
	deps := rt.prepareDeps(context.Background(), agent.Deps{})
	if deps.AttachBackend == nil {
		t.Fatal("prepareDeps dropped the attach callback")
	}
	if id, _ := deps.AttachBackend(context.Background(), nil, store.AttachPayload{}); id != "app" || !called {
		t.Error("the callback must be the one that was set")
	}
}
