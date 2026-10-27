package dashboard

// builder_revert_test.go — ai-frontend-builder T28: POST .../revert. Handler
// tests use the fake sandbox; the end-to-end test runs the revert turn with
// the real agent loop against the REAL builder server and git (skipped when
// git is missing), and checks that the new commit's tree equals the target
// turn's tree.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
	"github.com/zeeplabs/zeep-orbit/internal/builder"
	"go.uber.org/zap"
)

func (e *bhEnv) revert(t *testing.T, user *DashboardUser, sessionID, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, e.h.RevertSession, user, http.MethodPost,
		"/dashboard/api/builder/sessions/"+sessionID+"/revert", body, "id", sessionID)
}

// chatTurn runs a chat turn through the store that ends with commitSHA (or no
// commit), the way a worker would.
func (e *bhEnv) chatTurn(t *testing.T, sess BuilderSessionView, status, commitSHA string) *store.Turn {
	t.Helper()
	if _, err := e.st.CreateTurn(context.Background(), e.viewer.ID, sess.ID, store.KindChat, "change something"); err != nil {
		t.Fatalf("create turn: %v", err)
	}
	return e.finishTurn(t, status, commitSHA)
}

func TestBuilderRevert_CreatesPendingRevertTurnWithResolvedTarget(t *testing.T) {
	e := newBHEnv(t)
	pokes := e.withPoke()
	sess := e.createOK(t, e.viewer, e.appA) // turn 0: the template commit
	sha0 := sess.HeadSHA
	e.chatTurn(t, sess, store.TurnCompleted, "sha-turn-1")
	e.chatTurn(t, sess, store.TurnFailed, "")              // turn 2 made no commit
	e.chatTurn(t, sess, store.TurnCompleted, "sha-turn-3") // turn 3

	cases := []struct {
		turnSeq int
		wantSHA string
	}{
		{1, "sha-turn-1"},
		{2, "sha-turn-1"}, // a turn without a commit leaves the state of the one before
		{3, "sha-turn-3"},
		{0, sha0}, // the initial template
	}
	for i, tc := range cases {
		w := e.revert(t, e.viewer, sess.ID, fmt.Sprintf(`{"turn_seq":%d}`, tc.turnSeq))
		if w.Code != http.StatusAccepted {
			t.Fatalf("revert to %d: want 202, got %d: %s", tc.turnSeq, w.Code, w.Body.String())
		}
		got := decodeJSON[BuilderTurnView](t, w)
		if got.Kind != store.KindRevert || got.Status != store.TurnPending || got.Seq != 4+i {
			t.Errorf("revert to %d: turn = %+v, want a pending revert turn with seq %d", tc.turnSeq, got, 4+i)
		}
		turn, err := e.st.GetTurn(context.Background(), got.ID)
		if err != nil {
			t.Fatal(err)
		}
		target, ok := store.RevertTargetOf(turn)
		if !ok || target.TurnSeq != tc.turnSeq || target.SHA != tc.wantSHA {
			t.Errorf("revert to %d: target = %+v ok=%v, want sha %s", tc.turnSeq, target, ok, tc.wantSHA)
		}
		// No user message: a revert adds nothing to the conversation.
		if msgs, _ := e.st.ListMessages(context.Background(), sess.ID); len(msgs) != 3 {
			t.Errorf("messages = %d, a revert must not add any", len(msgs))
		}
		// Let the next iteration create another one.
		e.finishRevertTurn(t)
	}
	if pokes.Load() != int32(len(cases)) {
		t.Errorf("pokes = %d, want %d", pokes.Load(), len(cases))
	}
}

// finishRevertTurn completes the pending turn without running it.
func (e *bhEnv) finishRevertTurn(t *testing.T) {
	t.Helper()
	e.finishTurn(t, store.TurnCompleted, "")
}

func TestBuilderRevert_UnknownTurnSeqIs400(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)
	e.chatTurn(t, sess, store.TurnCompleted, "sha-turn-1")

	for name, body := range map[string]string{
		"beyond the last turn": `{"turn_seq":99}`,
		"negative":             `{"turn_seq":-1}`,
		"missing":              `{}`,
		"null":                 `{"turn_seq":null}`,
		"not a number":         `{"turn_seq":"one"}`,
		"not json":             `x`,
	} {
		w := e.revert(t, e.viewer, sess.ID, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}
	if got := bhErrorBody(t, e.revert(t, e.viewer, sess.ID, `{"turn_seq":99}`)); got != "unknown turn_seq" {
		t.Errorf("error = %q", got)
	}
	if e.turnCount(t, sess.ID) != 1 {
		t.Error("a rejected revert must not create a turn")
	}
}

func TestBuilderRevert_ActiveTurnIs409(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)
	e.chatTurn(t, sess, store.TurnCompleted, "sha-turn-1")
	if w := e.message(t, e.viewer, sess.ID, `{"content":"working on it"}`); w.Code != http.StatusAccepted {
		t.Fatalf("message: %d", w.Code)
	}

	w := e.revert(t, e.viewer, sess.ID, `{"turn_seq":1}`)
	if w.Code != http.StatusConflict || bhErrorBody(t, w) != "a turn is already in progress" {
		t.Fatalf("want 409, got %d: %s", w.Code, w.Body.String())
	}
	if e.turnCount(t, sess.ID) != 2 {
		t.Error("a rejected revert must not create a turn")
	}

	// And a revert in progress blocks messages and further reverts.
	e.finishRevertTurn(t)
	if w := e.revert(t, e.viewer, sess.ID, `{"turn_seq":1}`); w.Code != http.StatusAccepted {
		t.Fatalf("revert: %d %s", w.Code, w.Body.String())
	}
	if w := e.message(t, e.viewer, sess.ID, `{"content":"x"}`); w.Code != http.StatusConflict {
		t.Errorf("message during a revert: want 409, got %d", w.Code)
	}
	if w := e.revert(t, e.viewer, sess.ID, `{"turn_seq":1}`); w.Code != http.StatusConflict {
		t.Errorf("second revert: want 409, got %d", w.Code)
	}
}

func TestBuilderRevert_OwnerOnlyAuthAndAvailability(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)
	for name, u := range map[string]*DashboardUser{"outsider": e.outsider, "app admin": e.owner, "superadmin": e.super} {
		if w := e.revert(t, u, sess.ID, `{"turn_seq":0}`); w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", name, w.Code)
		}
	}
	if w := e.revert(t, nil, sess.ID, `{"turn_seq":0}`); w.Code != http.StatusUnauthorized {
		t.Errorf("no user: want 401, got %d", w.Code)
	}
	if w := e.revert(t, e.viewer, "nope", `{"turn_seq":0}`); w.Code != http.StatusNotFound {
		t.Errorf("malformed id: want 404, got %d", w.Code)
	}
	if e.turnCount(t, sess.ID) != 0 {
		t.Error("rejected requests must not create turns")
	}

	e.fake.mu.Lock()
	e.fake.healthErr = fmt.Errorf("dial tcp builder.internal:8080: refused")
	e.fake.mu.Unlock()
	w := e.revert(t, e.viewer, sess.ID, `{"turn_seq":0}`)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "builder.internal") {
		t.Errorf("builder down: want a generic 503, got %d: %s", w.Code, w.Body.String())
	}
	e2 := newBHEnv(t, bhWithoutBuilder())
	if w := e2.revert(t, e2.viewer, "11111111-1111-1111-1111-111111111111", `{"turn_seq":0}`); w.Code != http.StatusServiceUnavailable {
		t.Errorf("not configured: want 503, got %d", w.Code)
	}
}

func TestBuilderRevert_FailedSessionAcceptsRevert(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)
	e.chatTurn(t, sess, store.TurnCompleted, "sha-turn-1")
	e.chatTurn(t, sess, store.TurnFailed, "")
	if got := e.sessionStatus(t, sess.ID); got != store.SessionFailed {
		t.Fatalf("status = %s", got)
	}
	if w := e.revert(t, e.viewer, sess.ID, `{"turn_seq":1}`); w.Code != http.StatusAccepted {
		t.Errorf("want 202 on a failed session, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- end to end against the real builder ----

type neverProvider struct{ t *testing.T }

func (p neverProvider) Name() string  { return "never" }
func (p neverProvider) Model() string { return "never-1" }
func (p neverProvider) Chat(context.Context, provider.ChatRequest) (provider.ChatResponse, error) {
	p.t.Error("a revert turn must not call the model")
	return provider.ChatResponse{}, fmt.Errorf("unexpected model call")
}

type noBudget struct{ t *testing.T }

func (b noBudget) Check(context.Context, string, string) error {
	b.t.Error("a revert turn must not check the model budget")
	return nil
}

func archiveFiles(t *testing.T, sb sandbox.Sandbox, sessionID, sha string) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc, err := sb.Archive(ctx, sessionID, sha)
	if err != nil {
		t.Fatalf("archive %q: %v", sha, err)
	}
	defer rc.Close()
	files, err := bhReadTar(rc)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// newRealBuilderWithBuildOutput is newRealBuilderSandbox with an npx stand-in
// that "builds" by writing dist/index.html, which the builder requires of a
// successful build step; failBuild makes the stand-in exit 1 instead.
func newRealBuilderWithBuildOutput(t *testing.T, failBuild bool) sandbox.Sandbox {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	work, tmpl, ctl := t.TempDir(), t.TempDir(), t.TempDir()
	for rel, content := range bhDefaultTemplateFiles() {
		p := filepath.Join(tmpl, "default", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmpl, "default", ".gitignore"), []byte("node_modules/\ndist/\n.orbit/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := func(name, body string) string {
		p := filepath.Join(ctl, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	npx := "mkdir -p dist && echo built > dist/index.html"
	if failBuild {
		npx = "echo 'error TS2304: cannot find name Foo'; exit 1"
	}
	ex, err := builder.NewExecutor(builder.ExecConfig{
		NpmPath: script("npm", "exit 0"), NpxPath: script("npx", npx), Path: "/usr/bin:/bin", Home: ctl,
		WorkDir: work, MaxOutputTail: 4096, WaitDelay: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := builder.NewGitRepo(builder.GitConfig{HomeDir: filepath.Join(t.TempDir(), "home")})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := builder.NewSigner([]byte("dashboard-real-builder-test-secret-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := builder.NewServer(builder.ServerConfig{
		WorkDir: work, TemplatesDir: tmpl, Signer: signer, Executor: ex, Git: g,
		MaxActiveSessions: 4, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	sb, err := sandbox.NewHTTP(ts.URL, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	return sb
}

func TestBuilderRevert_AgainstRealBuilderRestoresTheTargetTree(t *testing.T) {
	realRevertScenario(t, false)
}

// A build that fails after the revert does not undo it: the tree is reverted,
// committed and snapshotted, and the event reports the failed build.
func TestBuilderRevert_AgainstRealBuilderFailedBuildStillReverts(t *testing.T) {
	realRevertScenario(t, true)
}

func realRevertScenario(t *testing.T, failBuild bool) {
	t.Helper()
	e := newBHEnv(t)
	real := newRealBuilderWithBuildOutput(t, failBuild) // skips when git is missing
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: real, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sess := e.createOK(t, e.viewer, e.appA)

	// Two chat turns that change the workspace, committed and snapshotted like
	// the loop does.
	runTurn := func(seq int, ops map[string]string) string {
		t.Helper()
		if _, err := e.st.CreateTurn(ctx, e.viewer.ID, sess.ID, store.KindChat, fmt.Sprintf("turn %d", seq)); err != nil {
			t.Fatal(err)
		}
		turn, err := e.st.ClaimTurn(ctx, "w", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		lease := store.Lease{TurnID: turn.ID, Owner: "w"}
		for path, content := range ops {
			if _, err := real.Op(ctx, sess.ID, fmt.Sprintf("t%d-%s", seq, path), sandbox.FileOp{Kind: sandbox.OpWriteFile, Path: path, Content: content}); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}
		c, err := real.Commit(ctx, sess.ID, seq, "turn "+turn.ID)
		if err != nil || !c.Changed {
			t.Fatalf("commit turn %d: %+v %v", seq, c, err)
		}
		if err := e.st.SaveSnapshot(ctx, lease, c.SHA, c.Bundle); err != nil {
			t.Fatal(err)
		}
		if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnCompleted, CommitSHA: c.SHA}); err != nil {
			t.Fatal(err)
		}
		return c.SHA
	}
	sha1 := runTurn(1, map[string]string{"src/one.txt": "one\n", "src/App.tsx": "export default function App() { return 1 }\n"})
	runTurn(2, map[string]string{"src/one.txt": "two\n", "src/two.txt": "only in turn 2\n"})
	tree1 := archiveFiles(t, real, sess.ID, sha1)
	if reflect.DeepEqual(tree1, archiveFiles(t, real, sess.ID, "")) {
		t.Fatal("test setup: turn 2 must differ from turn 1")
	}

	// The endpoint creates the revert turn...
	w := e.revert(t, e.viewer, sess.ID, `{"turn_seq":1}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("revert: %d %s", w.Code, w.Body.String())
	}
	view := decodeJSON[BuilderTurnView](t, w)
	if view.Seq != 3 || view.Kind != store.KindRevert {
		t.Fatalf("turn = %+v", view)
	}

	// ...and the worker (here: RunTurn on a claimed turn) executes it with no model.
	claimed, err := e.st.ClaimTurn(ctx, "w", time.Minute)
	if err != nil || claimed.ID != view.ID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	deps := agent.Deps{
		Store: e.st, Provider: neverProvider{t}, Tools: tools.NewRegistry(), Builder: real,
		Budget: noBudget{t}, Logger: nil,
	}
	if err := agent.RunTurn(ctx, deps, store.Lease{TurnID: claimed.ID, Owner: "w"}); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// New turn, completed, with its own commit; history kept (head moved forward).
	turn, err := e.st.GetTurn(ctx, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != store.TurnCompleted || turn.Kind != store.KindRevert || len(turn.CommitSHA) != 40 || turn.CommitSHA == sha1 {
		t.Fatalf("revert turn = %+v, want completed with a new commit", turn)
	}
	got := decodeJSON[BuilderSessionDetail](t, e.get(t, e.viewer, sess.ID))
	if got.HeadSHA != turn.CommitSHA || got.HeadTurnSeq != 3 || got.Status != store.SessionIdle || len(got.Turns) != 3 {
		t.Errorf("session = head %s seq %d status %s turns %d", got.HeadSHA, got.HeadTurnSeq, got.Status, len(got.Turns))
	}
	// The new commit's tree equals turn 1's tree.
	if head := archiveFiles(t, real, sess.ID, turn.CommitSHA); !reflect.DeepEqual(head, tree1) {
		t.Errorf("tree after revert = %v, want turn 1's tree %v", keysOf(head), keysOf(tree1))
	}
	if cur := archiveFiles(t, real, sess.ID, ""); !reflect.DeepEqual(cur, tree1) {
		t.Errorf("working tree after revert = %v, want turn 1's tree %v", keysOf(cur), keysOf(tree1))
	}
	// The old tip is still reachable: the revert is a new commit, not a rewrite.
	if old := archiveFiles(t, real, sess.ID, sha1); !reflect.DeepEqual(old, tree1) {
		t.Error("turn 1's commit changed")
	}

	// The revert commit is snapshotted, so a lost builder volume restores it.
	snaps, err := e.st.ListSnapshots(ctx, sess.ID)
	if err != nil || len(snaps) != 4 || snaps[3].TurnSeq != 3 || snaps[3].CommitSHA != turn.CommitSHA {
		t.Fatalf("snapshots = %d err %v, want turn 0, 1, 2 and the revert", len(snaps), err)
	}
	if err := real.Destroy(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := real.Restore(ctx, sess.ID, agent.BundlesFromSnapshots(snaps)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored := archiveFiles(t, real, sess.ID, ""); !reflect.DeepEqual(restored, tree1) {
		t.Errorf("restored workspace = %v, want turn 1's tree", keysOf(restored))
	}

	// The event log shows the revert, with a sanitized payload.
	page := decodeJSON[BuilderEventsPage](t, e.events(t, e.viewer, sess.ID, ""))
	var types []string
	var revertPayload string
	for _, ev := range page.Events {
		if ev.TurnID != nil && *ev.TurnID == view.ID {
			types = append(types, ev.Type)
			if ev.Type == agent.EventRevert {
				revertPayload = string(ev.Payload)
			}
		}
	}
	wantTypes, wantBuild := "turn_started,build_started,build_succeeded,revert,turn_finished", "succeeded"
	if failBuild {
		wantTypes, wantBuild = "turn_started,build_started,build_failed,revert,turn_finished", "failed"
	}
	if got := strings.Join(types, ","); got != wantTypes {
		t.Errorf("revert turn events = %s, want %s", got, wantTypes)
	}
	want := fmt.Sprintf(`{"build":%q,"changed":true,"sha":%q,"to_turn_seq":1}`, wantBuild, turn.CommitSHA)
	if revertPayload != want {
		t.Errorf("revert event payload = %s, want %s", revertPayload, want)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
