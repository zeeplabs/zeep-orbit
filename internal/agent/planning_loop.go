package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/skills"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

// The planning step of the loop (ai-builder-planning spec). A turn runs in
// plan mode (store.ModePlan) or build mode. In plan mode the model is offered
// only read tools and the two planning tools, and a call to anything else is
// answered with an error result without being dispatched: that second check
// is the guarantee, because a model can call a tool it was not offered (an
// earlier build turn of the session has write_file calls in its history).

// BriefInput is the session's current brief, as the prompt source read it.
type BriefInput struct {
	// Version is the brief version (1 for the first approved plan).
	Version int
	// Plan is the approved plan.
	Plan *planning.Plan
	// Done holds the 1-based milestones finished by a completed turn.
	Done map[int]bool
	// WorkspaceFile is the content of planning.BriefFileName in the workspace
	// at HEAD ("" when the file does not exist). A build turn rewrites the
	// file when it differs from the current brief.
	WorkspaceFile string
}

// Messages of the planning gate. They are tool results: the model reads them.
const (
	planModeRefusal = "%s is not available while planning: this turn cannot change files, run builds or propose backend changes. " +
		"Ask the user with ask_user if you still need something, then call propose_build_plan."
	questionsClosedRefusal = "the user ended the questions: do not ask again in this turn. " +
		"Call propose_build_plan with what you know and list every open point under assumptions."
	questionsLimitRefusalPlan = "question limit reached (%d rounds in this turn). " +
		"Call propose_build_plan with what you know and list every open point under assumptions."
	questionsLimitRefusalBuild = "question limit reached (%d round in a build turn). " +
		"Continue with what you know and tell the user what you assumed, or call propose_build_plan if the change is large."
)

// offered reports whether tools of kind k are available in this turn.
func (r *run) offered(k tools.Kind) bool {
	switch {
	case k == tools.KindPlanning:
		return r.planMode || r.deps.PlanningEnabled
	case r.planMode:
		return k == tools.KindReadOnly
	}
	return true
}

// toolSpecs is what the model is offered: the registered tools of the kinds
// this turn's mode allows, except load_skill when no skills are enabled (the
// tool lives on the shared registry, so the offer is decided per turn).
func (r *run) toolSpecs() []provider.ToolSpec {
	return r.deps.Tools.SpecsWhere(func(t tools.Tool) bool {
		if t.Name == skills.ToolLoadSkill && !r.skillsVisible {
			return false
		}
		return r.offered(t.Kind)
	})
}

// gatePlanning answers, without dispatching it, a call the turn's mode does
// not allow, and an ask_user call past the question bound. Every count comes
// from persisted tool messages (analyzeTurn), so a resume cannot reset it.
func (r *run) gatePlanning(c provider.ToolCall) (tools.Result, bool) {
	kind, ok := r.deps.Tools.Kind(c.Name)
	if !ok {
		return tools.Result{}, false // Dispatch answers "unknown tool"
	}
	if !r.offered(kind) {
		if r.planMode {
			return tools.Result{Content: fmt.Sprintf(planModeRefusal, c.Name), IsError: true}, true
		}
		// A planning tool while planning is turned off: it does not exist.
		return tools.Result{Content: fmt.Sprintf("unknown tool %q", c.Name), IsError: true}, true
	}
	if c.Name != planning.ToolAskUser {
		return tools.Result{}, false
	}
	if r.state.questionsClosed {
		return tools.Result{Content: questionsClosedRefusal, IsError: true}, true
	}
	limit, msg := planning.MaxBuildRounds, questionsLimitRefusalBuild
	if r.planMode {
		limit, msg = planning.MaxPlanRounds, questionsLimitRefusalPlan
	}
	if r.state.questionRounds >= limit+r.state.questionReopens {
		return tools.Result{Content: fmt.Sprintf(msg, limit), IsError: true}, true
	}
	return tools.Result{}, false
}

// usagePurpose is the ledger purpose of this turn's model calls.
func (r *run) usagePurpose() string {
	if r.planMode {
		return store.PurposePlanning
	}
	return store.PurposeChat
}

// directive is the per-turn instruction placed at the end of the session
// context: what the mode allows and, with a brief, where the plan stands.
func (r *run) directive(brief *BriefInput) string {
	var b strings.Builder
	if r.planMode {
		b.WriteString("Mode: planning. In this turn you cannot change files, run builds or propose backend changes; those tools are not available. " +
			"Find out what to build: call orbit_get_app_schema first and do not ask about data that already exists, " +
			"ask with ask_user only what the user's messages do not answer, then call propose_build_plan. " +
			"Do not reply in plain text: a question goes through ask_user, and the turn ends with propose_build_plan.")
		if brief != nil {
			b.WriteString(" An app brief already exists (APP BRIEF section): your plan replaces it, so send the whole plan and mark the milestones that are already built as done.")
		}
		return b.String()
	}
	if brief == nil {
		if r.deps.PlanningEnabled {
			b.WriteString("Mode: building. There is no app brief yet. If the request is a new app, a large change or ambiguous, " +
				"ask one round with ask_user or call propose_build_plan before editing; for a clear, small request just do it.")
		}
		return b.String()
	}

	current := 0
	if r.turn.Milestone != nil && r.turn.BriefVersion != nil && *r.turn.BriefVersion == brief.Version {
		current = *r.turn.Milestone
	}
	b.WriteString("Mode: building. Plan progress:\n")
	b.WriteString(planning.RenderProgress(brief.Plan, brief.Done, current))
	b.WriteString("\n")
	if current >= 1 && current <= len(brief.Plan.Milestones) {
		fmt.Fprintf(&b, "Build milestone %d only. Make the build pass, then stop and tell the user what was done", current)
		done := map[int]bool{current: true}
		for i := range brief.Done {
			done[i] = true
		}
		if next := brief.Plan.NextMilestone(done); next > 0 {
			fmt.Fprintf(&b, " and that milestone %d (%s) is next.", next, brief.Plan.Milestones[next-1].Title)
		} else {
			b.WriteString(" and that the plan is complete.")
		}
	} else {
		b.WriteString("This turn has no milestone: do what the user asks and do not start a milestone on your own.")
		if r.deps.PlanningEnabled {
			b.WriteString(" If the request changes what the brief says, call propose_build_plan with the revised plan.")
		}
	}
	return b.String()
}

// syncBriefFile writes the current brief to the workspace when a build turn
// finds the file missing or different (a new brief version, a revert, an edit
// by hand). The op id carries the turn and the version, so a resumed turn
// replays the write instead of repeating it; the file is committed with the
// turn's next checkpoint. A failure is logged and the turn goes on: the brief
// in the prompt is what the agent works from, the file is a copy for people.
func (r *run) syncBriefFile(ctx, tctx context.Context, brief *BriefInput) error {
	if r.planMode || brief == nil || brief.Plan == nil {
		return nil
	}
	want := planning.RenderBriefFile(brief.Plan, brief.Version)
	if want == brief.WorkspaceFile {
		return nil
	}
	opID := fmt.Sprintf("%s:brief-v%d", r.turn.ID, brief.Version)
	_, err := r.deps.Builder.Op(tctx, r.sess.ID, opID, sandbox.FileOp{Kind: sandbox.OpWriteFile, Path: planning.BriefFileName, Content: want})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.log.Error("agent brief file write failed", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
		return nil
	}
	r.wrote = true
	return nil
}
