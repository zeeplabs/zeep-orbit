package agent

// attach.go — the attach_backend turn (builder-no-backend NB-08, T11d-2). No
// model call is made: the worker writes the backend overlay, commits it, asks
// the deployment to attach the backend, and rebuilds so the preview has the SDK.
// See .specs/features/builder-no-backend/design.md ("T11b").

import (
	"context"
	"errors"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// EventBackendAttached is appended once the backend is attached. Its payload
// carries {mode, app_id, build, replaced}; see runAttach.
const EventBackendAttached = "backend_attached"

// Error codes of an attach turn.
const (
	CodeAttachFailed    = "attach_failed"
	CodeOverlayConflict = "overlay_conflict"
	// CodeAttachUnavailable: the deployment gave the worker no way to attach.
	CodeAttachUnavailable = "attach_unavailable"
)

// attachCleanupTimeout bounds the compensation, which must run even when the
// turn's own context is already spent.
const attachCleanupTimeout = 2 * time.Minute

// AttachFunc attaches the backend the payload describes to the session and
// returns the id of the backend app. For a new backend it creates the app. It
// must be idempotent: a turn resumed after a crash calls it again, and it must
// then return the app the first call attached. When it fails after creating
// an app it removes the app itself.
type AttachFunc func(ctx context.Context, sess *store.Session, p store.AttachPayload) (appID string, err error)

// runAttach executes an attach turn:
//
//  1. Overlay: the SDK files, the dependency and the guide go into the workspace
//     (nothing is written when a file already exists).
//  2. Commit and snapshot, so the overlay is safe before anything else changes.
//  3. Deps.AttachBackend: create the app (new) and set the session's backend.
//  4. Install and build, so the preview has the SDK.
//  5. A `backend_attached` event, then the turn completes.
//
// When step 3 fails the workspace is reverted to the head the session had (as a
// new commit, snapshotted) so it never keeps SDK files while the session says it
// has no backend. A failed build does NOT fail the turn: the backend is
// attached, and the event says the preview may be stale.
func (r *run) runAttach(ctx context.Context) (*outcome, error) {
	if err := r.emit(ctx, EventTurnStarted, map[string]any{"seq": r.turn.Seq, "kind": store.KindAttachBackend}); err != nil {
		return nil, err
	}
	p, ok := store.AttachPayloadOf(r.turn)
	if !ok {
		r.log.Error("agent attach turn without a payload", "turn", r.turn.ID, "session", r.sess.ID)
		return &outcome{status: store.TurnFailed, code: CodeInternal}, nil
	}
	if r.deps.AttachBackend == nil {
		r.log.Error("agent attach turn without Deps.AttachBackend", "turn", r.turn.ID, "session", r.sess.ID)
		return &outcome{status: store.TurnFailed, code: CodeAttachUnavailable}, nil
	}

	tctx, cancel := context.WithTimeout(ctx, r.lim.MaxTurnDuration)
	defer cancel()

	// The head the workspace has before this turn writes anything: the
	// compensation restores it.
	base := r.sess.HeadSHA

	ov, err := r.deps.Builder.ApplyBackendOverlay(tctx, r.sess.ID, "attach-"+r.turn.ID,
		map[string]string{"src/orbit.config.ts": p.Config})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if sandbox.IsOverlayConflict(err) {
			// Nothing was written: the builder checks every path first.
			return &outcome{status: store.TurnFailed, code: CodeOverlayConflict}, nil
		}
		r.log.Error("agent attach overlay failed", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
		// A write that failed half way leaves uncommitted files behind.
		if cerr := r.discardOverlay(ctx, base); cerr != nil {
			return nil, cerr
		}
		if errors.Is(err, sandbox.ErrWorkspaceLost) {
			return &outcome{status: store.TurnFailed, code: CodeWorkspaceLost}, nil
		}
		return &outcome{status: store.TurnFailed, code: CodeAttachFailed}, nil
	}

	res, err := r.deps.Builder.Commit(tctx, r.sess.ID, r.turn.Seq, "turn "+r.turn.ID)
	if err == nil {
		err = r.snapshot(ctx, res)
	}
	if err != nil {
		if errors.Is(err, store.ErrLeaseLost) || ctx.Err() != nil {
			return nil, err
		}
		r.log.Error("agent attach commit failed", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
		// No app exists and the row still says none. The overlay may be
		// committed without a snapshot or not committed at all: put the
		// workspace back either way.
		if cerr := r.discardOverlay(ctx, base); cerr != nil {
			return nil, cerr
		}
		if errors.Is(err, sandbox.ErrWorkspaceLost) {
			return &outcome{status: store.TurnFailed, code: CodeWorkspaceLost}, nil
		}
		return &outcome{status: store.TurnFailed, code: CodeCommitFailed}, nil
	}
	if base == "" {
		base = res.Prev
	}

	appID, err := r.deps.AttachBackend(tctx, r.sess, p)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		r.log.Error("agent attach backend failed", "turn", r.turn.ID, "session", r.sess.ID, "mode", p.Mode, "err", err)
		if cerr := r.discardOverlay(ctx, base); cerr != nil {
			return nil, cerr
		}
		return &outcome{status: store.TurnFailed, code: CodeAttachFailed}, nil
	}

	build, err := r.attachBuild(ctx, tctx)
	if err != nil {
		return nil, err
	}

	sha := ""
	if res.SHA != "" && res.SHA != r.sess.HeadSHA {
		sha = res.SHA
	}
	if err := r.emit(ctx, EventBackendAttached, map[string]any{
		"mode": p.Mode, "app_id": appID, "build": build, "replaced": ov.Replaced,
	}); err != nil {
		return nil, err
	}
	return &outcome{status: store.TurnCompleted, sha: sha}, nil
}

// attachBuild installs and builds the workspace and returns the RevertBuild*
// status of the first step that did not succeed, else RevertBuildSucceeded. The
// error is only a cancellation of ctx or a lost lease.
func (r *run) attachBuild(ctx, tctx context.Context) (string, error) {
	for _, step := range []sandbox.BuildStep{sandbox.StepInstall, sandbox.StepBuild} {
		status, err := r.buildStep(ctx, tctx, step, "attach-"+r.turn.ID+"-"+string(step))
		if err != nil || status != RevertBuildSucceeded {
			return status, err
		}
	}
	return RevertBuildSucceeded, nil
}

// discardOverlay puts the workspace back to base (a new revert commit when the
// overlay was committed, a clean tree when it was not) and snapshots the result.
// A failure to do so is logged and swallowed: the turn still fails with its own
// code, and the leftover is files the next turn's commit picks up. The error is
// only a lost lease or a cancellation of ctx.
func (r *run) discardOverlay(ctx context.Context, base string) error {
	if base == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attachCleanupTimeout)
	defer cancel()
	res, err := r.deps.Builder.Revert(cctx, r.sess.ID, base, r.turn.Seq)
	if err != nil {
		r.log.Error("agent attach cleanup failed", "turn", r.turn.ID, "session", r.sess.ID, "to", base, "err", err)
		return ctx.Err()
	}
	if err := r.snapshot(cctx, res); err != nil {
		if errors.Is(err, store.ErrLeaseLost) {
			return err
		}
		r.log.Error("agent attach cleanup snapshot failed", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
	}
	return ctx.Err()
}
