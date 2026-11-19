package agent

// revert.go — the revert turn (ai-frontend-builder T28, AFB-10). No model call
// is made: the worker restores the workspace to the state an earlier turn left
// it in, snapshots the result and rebuilds the preview.

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

// EventRevert is appended once the workspace is restored. Its payload carries
// {to_turn_seq, sha, changed, build}; see runRevert.
const EventRevert = "revert"

// CodeRevertFailed is the generic error code of a revert turn whose builder
// call failed.
const CodeRevertFailed = "revert_failed"

// Build outcomes reported in the `revert` event.
const (
	RevertBuildSucceeded = "succeeded"
	RevertBuildFailed    = "failed"      // the build ran and failed (or timed out)
	RevertBuildError     = "unavailable" // the builder could not run the build
)

// runRevert executes a revert turn:
//
//  1. Sandbox.Revert to the target commit, as a NEW commit whose tree equals
//     the target (history is never rewritten).
//  2. Snapshot the new commit right away, so a builder volume loss after this
//     point cannot lose the revert.
//  3. Rebuild, so the preview matches the restored tree.
//  4. A `revert` event with the build status, then the turn completes.
//
// A failed or unavailable build does NOT fail the turn: the tree is reverted
// and committed, which is what the user asked for; the event says that the
// preview may be stale. Only a failure to revert or to snapshot fails it.
func (r *run) runRevert(ctx context.Context) (*outcome, error) {
	if err := r.emit(ctx, EventTurnStarted, map[string]any{"seq": r.turn.Seq, "kind": store.KindRevert}); err != nil {
		return nil, err
	}
	target, ok := store.RevertTargetOf(r.turn)
	if !ok {
		r.log.Error("agent revert turn without a target", "turn", r.turn.ID, "session", r.sess.ID)
		return &outcome{status: store.TurnFailed, code: CodeInternal}, nil
	}

	tctx, cancel := context.WithTimeout(ctx, r.lim.MaxTurnDuration)
	defer cancel()

	res, err := r.deps.Builder.Revert(tctx, r.sess.ID, target.SHA, r.turn.Seq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		r.log.Error("agent revert failed", "turn", r.turn.ID, "session", r.sess.ID, "to_seq", target.TurnSeq, "err", err)
		if errors.Is(err, sandbox.ErrWorkspaceLost) {
			return &outcome{status: store.TurnFailed, code: CodeWorkspaceLost}, nil
		}
		return &outcome{status: store.TurnFailed, code: CodeRevertFailed}, nil
	}
	if err := r.snapshot(ctx, res); err != nil {
		if errors.Is(err, store.ErrLeaseLost) || ctx.Err() != nil {
			return nil, err
		}
		r.log.Error("agent revert snapshot failed", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
		return &outcome{status: store.TurnFailed, code: CodeCommitFailed}, nil
	}

	build, err := r.revertBuild(ctx, tctx)
	if err != nil {
		return nil, err
	}

	// An unchanged tree adds no commit, so the head only moves when the builder
	// already was ahead of the session (a resumed turn), like the chat turns.
	sha := ""
	if res.SHA != "" && res.SHA != r.sess.HeadSHA {
		sha = res.SHA
	}
	if err := r.emit(ctx, EventRevert, map[string]any{
		"to_turn_seq": target.TurnSeq, "sha": sha, "changed": res.Changed, "build": build,
	}); err != nil {
		return nil, err
	}
	return &outcome{status: store.TurnCompleted, sha: sha}, nil
}

// revertBuild rebuilds the restored tree and returns the RevertBuild* status.
// The error is only a cancellation of ctx or a lost lease.
func (r *run) revertBuild(ctx, tctx context.Context) (string, error) {
	const step = sandbox.StepBuild
	if err := r.emit(ctx, EventBuildStarted, map[string]any{"step": string(step)}); err != nil {
		return "", err
	}
	previewBase := tools.PreviewBuildBase
	if r.deps.PreviewBase != nil {
		previewBase = r.deps.PreviewBase
	}
	res, err := r.deps.Builder.Build(tctx, r.sess.ID, "revert-"+r.turn.ID+"-build", step, previewBase(r.sess.ID))
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return "", ctx.Err()
	case tctx.Err() != nil:
		return RevertBuildFailed, r.emit(ctx, EventBuildFailed, map[string]any{"step": string(step), "tail": "build timed out"})
	default:
		r.log.Error("agent revert build failed to run", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
		return RevertBuildError, nil
	}
	if res.ExitCode != 0 || res.TimedOut {
		tail := tailRunes(res.OutputTail, maxBuildTailChars)
		if res.TimedOut {
			tail = "build timed out"
		}
		if err := r.emit(ctx, EventBuildFailed, map[string]any{"step": string(step), "tail": tail}); err != nil {
			return "", fmt.Errorf("revert build event: %w", err)
		}
		return RevertBuildFailed, nil
	}
	if err := r.emit(ctx, EventBuildSucceeded, map[string]any{"step": string(step)}); err != nil {
		return "", err
	}
	return RevertBuildSucceeded, nil
}
