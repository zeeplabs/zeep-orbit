package dashboard

// builder_planning.go — the user's side of the planning mode (ai-builder-planning
// P06, design §5 and §8). A plan-mode turn pauses on ask_user (a question round)
// or propose_build_plan (a build plan); these routes resolve it. Methods of
// *BuilderHandler, mounted by the router under /dashboard behind RequireAuth and
// the AI chat limiter (they resume a turn, which spends tokens):
//
//	POST /api/builder/sessions/{id}/turns/{turn}/answers        AnswerQuestions
//	POST /api/builder/sessions/{id}/turns/{turn}/plan/approve   ApprovePlan
//	POST /api/builder/sessions/{id}/turns/{turn}/plan/revise    RevisePlan
//
// Owner only, like the other session routes. They change no backend app, so no
// app role is checked: approving a plan starts a frontend build, as sending a
// message does.
//
// Every body names the tool_call_id of the card the user acted on. A turn can
// pause several times (one round after another, a revised plan), and a click
// on a card that was already replaced must not answer the next round or approve
// a plan the user has not seen: a different tool_call_id is a 409. A repeated
// request for a call that was already resolved the same way answers 200 with
// already_resolved, so a double click is harmless.
//
// confirm and reject (builder_proposals.go) refuse a planning tool, and these
// routes refuse a backend proposal, both with 409.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// Limits and public messages of the planning routes.
const (
	// builderPlanningBodyLimit caps the body: at most planning.MaxQuestionsPerRound
	// answers of planning.MaxAnswerTextChars runes, or planning.MaxFeedbackChars.
	builderPlanningBodyLimit = 32 << 10
	// builderAnswerEventChars caps each answer kept in the event log.
	builderAnswerEventChars = 200

	builderToolCallRequiredMsg   = "tool_call_id is required"
	builderPlanningStaleMsg      = "this card was replaced by a newer one; reload the session"
	builderNotQuestionRoundMsg   = "the pending proposal is not a question round"
	builderNotBuildPlanMsg       = "the pending proposal is not a build plan"
	builderPlanMalformedMsg      = "the plan is malformed; ask the agent for changes"
	builderQuestionsMalformed    = "the questions are malformed; start building now or send a new message"
	builderProposalIsPlanningMsg = "the pending proposal is a question round or a build plan; answer it from its card"
)

var (
	// errPlanningStale: the request names another tool call than the pending one.
	errPlanningStale = errors.New("dashboard: builder: planning card replaced")
	// errPlanningMalformed: the stored arguments no longer parse. The turn stays
	// paused; the user can still revise the plan or skip the questions.
	errPlanningMalformed = errors.New("dashboard: builder: stored planning arguments do not parse")
)

// builderPlanningInputError is a 400 whose message is safe to show (the
// validators of the planning package answer in English).
type builderPlanningInputError struct{ msg string }

func (e *builderPlanningInputError) Error() string { return e.msg }

// builderPlanningResult is the body of the planning routes.
type builderPlanningResult struct {
	// AlreadyResolved marks a repeated request that did nothing.
	AlreadyResolved bool `json:"already_resolved,omitempty"`
	// BriefVersion and Milestone describe an approved plan: the brief version it
	// became and the milestone the turn builds now (0 when none is pending).
	BriefVersion int              `json:"brief_version,omitempty"`
	Milestone    int              `json:"milestone,omitempty"`
	Turn         *BuilderTurnView `json:"turn,omitempty"`
}

type answerQuestionsRequest struct {
	ToolCallID string            `json:"tool_call_id"`
	Answers    []planning.Answer `json:"answers"`
	// Skip is "Start building now": the agent may not ask again in this turn
	// and proposes the plan with what it has. Answers sent with it are kept.
	Skip bool `json:"skip"`
}

type approvePlanRequest struct {
	ToolCallID string `json:"tool_call_id"`
}

type revisePlanRequest struct {
	ToolCallID    string `json:"tool_call_id"`
	Feedback      string `json:"feedback"`
	MoreQuestions bool   `json:"more_questions"`
}

// retryWhileBusy runs resolve again while another request holds the proposal
// (store.ErrProposalBusy), like resolveProposal.
func retryWhileBusy(ctx context.Context, resolve func() error) error {
	for attempt := 0; ; attempt++ {
		err := resolve()
		if !errors.Is(err, store.ErrProposalBusy) || attempt >= proposalBusyRetries {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(proposalBusyWait):
		}
	}
}

// planningRoute is the prologue of the planning routes: caller, session, turn
// and the decoded body. ok=false means the response was written.
func (h *BuilderHandler) planningRoute(w http.ResponseWriter, r *http.Request, body any, toolCallID func() string) (*DashboardUser, *store.Session, *store.Turn, bool) {
	user, sess, turn, ok := h.proposalRoute(w, r)
	if !ok {
		return nil, nil, nil, false
	}
	if !h.decodeBuilderBody(w, r, body, builderPlanningBodyLimit) {
		return nil, nil, nil, false
	}
	if strings.TrimSpace(toolCallID()) == "" {
		h.writeError(w, http.StatusBadRequest, builderToolCallRequiredMsg)
		return nil, nil, nil, false
	}
	return user, sess, turn, true
}

// planningResolved maps the error of a planning resolution to a response.
// done=true means the request succeeded and the caller writes the result.
func (h *BuilderHandler) planningResolved(w http.ResponseWriter, r *http.Request, turn *store.Turn, err error,
	eventType, toolCallID, wrongKindMsg string) (done bool) {
	var inputErr *builderPlanningInputError
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrProposalNotPending):
		_, resolved, lerr := h.store.ResolvedToolCall(r.Context(), turn.ID, eventType, toolCallID)
		if lerr != nil {
			h.serverError(w, r, lerr)
			return false
		}
		if resolved {
			writeJSON(w, http.StatusOK, builderPlanningResult{AlreadyResolved: true})
			return false
		}
		h.writeError(w, http.StatusConflict, builderProposalNotPending)
	case errors.Is(err, errPlanningStale):
		h.writeError(w, http.StatusConflict, builderPlanningStaleMsg)
	case errors.Is(err, store.ErrProposalKind):
		h.writeError(w, http.StatusConflict, wrongKindMsg)
	case errors.Is(err, store.ErrProposalBusy):
		h.writeError(w, http.StatusConflict, builderProposalBusyMsg)
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrSessionArchived):
		h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
	case errors.As(err, &inputErr):
		h.writeError(w, http.StatusBadRequest, inputErr.msg)
	case errors.Is(err, errPlanningMalformed):
		if eventType == store.EventPlanApproved {
			h.writeError(w, http.StatusBadRequest, builderPlanMalformedMsg)
		} else {
			h.writeError(w, http.StatusBadRequest, builderQuestionsMalformed)
		}
	default:
		h.serverError(w, r, err)
	}
	return false
}

// AnswerQuestions handles POST .../turns/{turn}/answers.
func (h *BuilderHandler) AnswerQuestions(w http.ResponseWriter, r *http.Request) {
	var req answerQuestionsRequest
	user, sess, turn, ok := h.planningRoute(w, r, &req, func() string { return req.ToolCallID })
	if !ok {
		return
	}
	var resolved *store.Turn
	err := retryWhileBusy(r.Context(), func() error {
		var err error
		resolved, err = h.store.AnswerQuestions(r.Context(), user.ID, sess.ID, turn.ID,
			func(_ context.Context, p store.PendingProposal) (*store.PlanningResolution, error) {
				if p.ToolCallID != req.ToolCallID {
					return nil, errPlanningStale
				}
				payload := map[string]any{"tool_call_id": p.ToolCallID, "skipped": req.Skip}
				res := &store.PlanningResolution{EventPayload: payload, QuestionsClosed: req.Skip}
				questions, err := planning.ParseQuestions(p.Arguments)
				if err != nil {
					if !req.Skip {
						return nil, errPlanningMalformed
					}
					// Skipping needs nothing from the questions.
					res.Content = planning.SkippedResult
					return res, nil
				}
				answers, err := planning.NormalizeAnswers(questions, req.Answers)
				if err != nil {
					return nil, &builderPlanningInputError{msg: err.Error()}
				}
				payload["answers"] = planning.AnswersSummary(questions, answers, builderAnswerEventChars)
				switch {
				case !req.Skip:
					res.Content = planning.AnswersResult(questions, answers)
				case anyAnswered(answers):
					res.Content = planning.AnswersResult(questions, answers) + "\n\n" + planning.SkippedResult
				default:
					res.Content = planning.SkippedResult
				}
				return res, nil
			})
		return err
	})
	if !h.planningResolved(w, r, turn, err, store.EventQuestionsAnswered, req.ToolCallID, builderNotQuestionRoundMsg) {
		return
	}
	if h.poke != nil {
		h.poke()
	}
	// Answers are chat content: the audit row records only that a round was
	// answered.
	h.audit(r.Context(), user, "builder.questions.answer", sess.ID, sess.Title,
		map[string]any{"turn_seq": turn.Seq, "skipped": req.Skip}, r.RemoteAddr)
	view := newBuilderTurnView(resolved)
	writeJSON(w, http.StatusAccepted, builderPlanningResult{Turn: &view})
}

func anyAnswered(answers []planning.Answer) bool {
	for _, a := range answers {
		if !a.Decide {
			return true
		}
	}
	return false
}

// ApprovePlan handles POST .../turns/{turn}/plan/approve. The plan becomes the
// next brief version and the same turn goes on building its first pending
// milestone (store.ApproveBuildPlan, one transaction).
func (h *BuilderHandler) ApprovePlan(w http.ResponseWriter, r *http.Request) {
	var req approvePlanRequest
	user, sess, turn, ok := h.planningRoute(w, r, &req, func() string { return req.ToolCallID })
	if !ok {
		return
	}
	var (
		resolved   *store.Turn
		brief      *store.Brief
		milestones int
		milestone  int
	)
	err := retryWhileBusy(r.Context(), func() error {
		var err error
		resolved, brief, err = h.store.ApproveBuildPlan(r.Context(), user.ID, sess.ID, turn.ID,
			func(_ context.Context, p store.PendingProposal) (*store.PlanApproval, error) {
				if p.ToolCallID != req.ToolCallID {
					return nil, errPlanningStale
				}
				// Re-parse with the validator the tool used: what is stored is
				// exactly what was validated, normalized.
				plan, err := planning.ParsePlan(p.Arguments)
				if err != nil {
					return nil, errPlanningMalformed
				}
				normalized, err := json.Marshal(plan)
				if err != nil {
					return nil, err
				}
				milestones, milestone = len(plan.Milestones), plan.NextMilestone(nil)
				return &store.PlanApproval{
					Plan: normalized, Milestones: milestones, Milestone: milestone,
					Content: func(version int) string { return planning.ApprovedResult(plan, version, milestone) },
				}, nil
			})
		return err
	})
	if !h.planningResolved(w, r, turn, err, store.EventPlanApproved, req.ToolCallID, builderNotBuildPlanMsg) {
		return
	}
	if h.poke != nil {
		h.poke()
	}
	h.audit(r.Context(), user, "builder.plan.approve", sess.ID, sess.Title,
		map[string]any{"turn_seq": turn.Seq, "version": brief.Version, "milestones": milestones}, r.RemoteAddr)
	view := newBuilderTurnView(resolved)
	writeJSON(w, http.StatusAccepted, builderPlanningResult{BriefVersion: brief.Version, Milestone: milestone, Turn: &view})
}

// RevisePlan handles POST .../turns/{turn}/plan/revise: the plan is not
// approved; the agent reads the user's words and proposes again, or asks one
// more round first (more_questions).
func (h *BuilderHandler) RevisePlan(w http.ResponseWriter, r *http.Request) {
	var req revisePlanRequest
	user, sess, turn, ok := h.planningRoute(w, r, &req, func() string { return req.ToolCallID })
	if !ok {
		return
	}
	content, err := planning.RevisionResult(req.Feedback, req.MoreQuestions)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var resolved *store.Turn
	err = retryWhileBusy(r.Context(), func() error {
		var err error
		resolved, err = h.store.ReviseBuildPlan(r.Context(), user.ID, sess.ID, turn.ID,
			func(_ context.Context, p store.PendingProposal) (*store.PlanningResolution, error) {
				if p.ToolCallID != req.ToolCallID {
					return nil, errPlanningStale
				}
				return &store.PlanningResolution{
					Content:           content,
					EventPayload:      map[string]any{"tool_call_id": p.ToolCallID, "more_questions": req.MoreQuestions},
					QuestionsReopened: req.MoreQuestions,
				}, nil
			})
		return err
	})
	if !h.planningResolved(w, r, turn, err, store.EventPlanRevisionRequested, req.ToolCallID, builderNotBuildPlanMsg) {
		return
	}
	if h.poke != nil {
		h.poke()
	}
	h.audit(r.Context(), user, "builder.plan.revise", sess.ID, sess.Title,
		map[string]any{"turn_seq": turn.Seq, "more_questions": req.MoreQuestions}, r.RemoteAddr)
	view := newBuilderTurnView(resolved)
	writeJSON(w, http.StatusAccepted, builderPlanningResult{Turn: &view})
}

// BuilderMilestoneView is one milestone of the session's brief.
type BuilderMilestoneView struct {
	Index int    `json:"index"`
	Title string `json:"title"`
}

// builderBriefProgress is the brief part of the session detail: the current
// brief version (0 when there is none) and its milestone progress.
type builderBriefProgress struct {
	BriefVersion    int                   `json:"brief_version"`
	MilestonesTotal int                   `json:"milestones_total"`
	MilestonesDone  int                   `json:"milestones_done"`
	NextMilestone   *BuilderMilestoneView `json:"next_milestone"`
}

// briefProgress reads the brief progress of a session.
func (h *BuilderHandler) briefProgress(ctx context.Context, sessionID string) (builderBriefProgress, error) {
	var out builderBriefProgress
	brief, states, err := h.store.MilestoneProgress(ctx, sessionID)
	if err != nil || brief == nil {
		return out, err
	}
	out.BriefVersion, out.MilestonesTotal = brief.Version, len(states)
	for _, m := range states {
		if m.Done {
			out.MilestonesDone++
		} else if out.NextMilestone == nil {
			out.NextMilestone = &BuilderMilestoneView{Index: m.Index, Title: m.Title}
		}
	}
	return out, nil
}
