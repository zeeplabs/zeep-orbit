package store

// Planning (ai-builder-planning spec): the session phase, the turn mode, the
// versioned brief, and the resolutions of the two planning tools.
//
// A turn paused on ask_user or propose_build_plan uses the same parking as a
// backend proposal (builder_turns.pending_proposal, status
// awaiting_confirmation) and is resumed by the methods below, which share the
// transaction of ResolveProposal: the tool result the agent reads, the event
// and, for an approval, the new brief version and the move to the building
// phase are written together or not at all.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Session phases (builder_sessions.phase).
const (
	PhaseBriefing = "briefing"
	PhaseBuilding = "building"
)

// Turn modes (builder_turns.mode). A plan-mode turn is offered only read and
// planning tools; it becomes a build turn when its plan is approved.
const (
	ModePlan  = "plan"
	ModeBuild = "build"
)

// Names of the planning tools, as parked in pending_proposal. They equal
// planning.ToolAskUser and planning.ToolProposeBuildPlan (asserted by a test in
// the agent package; this package does not import the tools).
const (
	PlanningToolAskUser          = "ask_user"
	PlanningToolProposeBuildPlan = "propose_build_plan"
)

// Event types written by the planning resolutions.
const (
	EventQuestionsAnswered     = "questions_answered"
	EventPlanApproved          = "plan_approved"
	EventPlanRevisionRequested = "plan_revision_requested"
)

var (
	// ErrProposalKind: the turn is awaiting confirmation, but on a tool this
	// resolution does not handle (a backend proposal sent to a planning
	// resolution, or the reverse).
	ErrProposalKind = errors.New("the pending proposal is of another kind")
	// ErrMilestoneMismatch: the milestone a turn was asked to build is not the
	// next pending milestone of the session's current brief.
	ErrMilestoneMismatch = errors.New("not the next pending milestone")
)

func isPlanningTool(name string) bool {
	return name == PlanningToolAskUser || name == PlanningToolProposeBuildPlan
}

// Brief is a row of zeep_system.builder_briefs: one approved build plan. The
// newest version is the session's brief.
type Brief struct {
	SessionID string
	Version   int
	// Plan is the approved, normalized propose_build_plan arguments.
	Plan      json.RawMessage
	TurnID    *string
	CreatedBy string
	CreatedAt time.Time
}

// BriefVersion is one entry of a session's brief history.
type BriefVersion struct {
	Version   int
	CreatedAt time.Time
}

// MilestoneState is one milestone of the current brief and whether it is done.
type MilestoneState struct {
	// Index is 1-based.
	Index int
	Title string
	Done  bool
}

// querier is the read surface shared by the pool and a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const briefColumns = `session_id, version, plan, turn_id, created_by, created_at`

func scanBrief(row pgx.Row) (*Brief, error) {
	var b Brief
	if err := row.Scan(&b.SessionID, &b.Version, &b.Plan, &b.TurnID, &b.CreatedBy, &b.CreatedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

func currentBrief(ctx context.Context, q querier, sessionID string) (*Brief, error) {
	b, err := scanBrief(q.QueryRow(ctx,
		`SELECT `+briefColumns+` FROM zeep_system.builder_briefs
		 WHERE session_id = $1 ORDER BY version DESC LIMIT 1`, sessionID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("builder store: current brief: %w", err)
	}
	return b, nil
}

// CurrentBrief returns the newest brief of the session, or nil when it has
// none. Internal (not owner-scoped), like GetSessionByID.
func (s *Store) CurrentBrief(ctx context.Context, sessionID string) (*Brief, error) {
	return currentBrief(ctx, s.pool, sessionID)
}

// GetBrief returns one version of the session's brief, or ErrNotFound.
// Internal (not owner-scoped).
func (s *Store) GetBrief(ctx context.Context, sessionID string, version int) (*Brief, error) {
	b, err := scanBrief(s.pool.QueryRow(ctx,
		`SELECT `+briefColumns+` FROM zeep_system.builder_briefs WHERE session_id = $1 AND version = $2`,
		sessionID, version))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("builder store: get brief: %w", err)
	}
	return b, nil
}

// ListBriefVersions returns the versions of the session's brief, newest first.
// Internal (not owner-scoped).
func (s *Store) ListBriefVersions(ctx context.Context, sessionID string) ([]BriefVersion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT version, created_at FROM zeep_system.builder_briefs WHERE session_id = $1 ORDER BY version DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("builder store: list brief versions: %w", err)
	}
	defer rows.Close()
	out := []BriefVersion{}
	for rows.Next() {
		var v BriefVersion
		if err := rows.Scan(&v.Version, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("builder store: scan brief version: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("builder store: list brief versions: %w", err)
	}
	return out, nil
}

// MilestoneProgress returns the session's current brief and the state of each
// of its milestones. A milestone is done when the plan marks it done or a turn
// that built it for this brief version ended completed, so there is no stored
// counter to disagree with the turn history. It returns (nil, nil, nil) when
// the session has no brief. Internal (not owner-scoped).
func (s *Store) MilestoneProgress(ctx context.Context, sessionID string) (*Brief, []MilestoneState, error) {
	return milestoneProgress(ctx, s.pool, sessionID)
}

func milestoneProgress(ctx context.Context, q querier, sessionID string) (*Brief, []MilestoneState, error) {
	brief, err := currentBrief(ctx, q, sessionID)
	if err != nil || brief == nil {
		return nil, nil, err
	}
	var plan struct {
		Milestones []struct {
			Title string `json:"title"`
			Done  bool   `json:"done"`
		} `json:"milestones"`
	}
	if err := json.Unmarshal(brief.Plan, &plan); err != nil {
		return nil, nil, fmt.Errorf("builder store: milestone progress: brief v%d is not valid JSON: %w", brief.Version, err)
	}
	rows, err := q.Query(ctx,
		`SELECT DISTINCT milestone FROM zeep_system.builder_turns
		 WHERE session_id = $1 AND brief_version = $2 AND milestone IS NOT NULL AND status = 'completed'`,
		sessionID, brief.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("builder store: milestone progress: %w", err)
	}
	defer rows.Close()
	built := map[int]bool{}
	for rows.Next() {
		var m int
		if err := rows.Scan(&m); err != nil {
			return nil, nil, fmt.Errorf("builder store: milestone progress: scan: %w", err)
		}
		built[m] = true
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("builder store: milestone progress: %w", err)
	}
	states := make([]MilestoneState, 0, len(plan.Milestones))
	for i, m := range plan.Milestones {
		states = append(states, MilestoneState{Index: i + 1, Title: m.Title, Done: m.Done || built[i+1]})
	}
	return brief, states, nil
}

// nextPending returns the 1-based index of the first milestone that is not
// done, or 0 when all are.
func nextPending(states []MilestoneState) int {
	for _, m := range states {
		if !m.Done {
			return m.Index
		}
	}
	return 0
}

// NextPendingMilestone is nextPending for callers outside the package.
func NextPendingMilestone(states []MilestoneState) int { return nextPending(states) }

// ResolvedToolCall returns the payload of the latest eventType event of turnID
// that resolved the tool call toolCallID, and whether there is one. The
// planning routes use it to answer a repeated request as already resolved.
// Internal (not owner-scoped): call it only for a turn of a session the caller
// already authorized.
func (s *Store) ResolvedToolCall(ctx context.Context, turnID, eventType, toolCallID string) (json.RawMessage, bool, error) {
	var payload json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT payload FROM zeep_system.builder_events
		 WHERE turn_id = $1 AND type = $2 AND payload->>'tool_call_id' = $3
		 ORDER BY id DESC LIMIT 1`, turnID, eventType, toolCallID,
	).Scan(&payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("builder store: resolved tool call: %w", err)
	}
	return payload, true, nil
}

// PlanningResolution is what the decide of AnswerQuestions and ReviseBuildPlan
// returns.
type PlanningResolution struct {
	// Content is the tool result text the agent reads when the turn resumes.
	Content string
	// EventPayload is the event payload (capped user text only).
	EventPayload any
	// QuestionsClosed: the user ended the questions; the agent may not ask
	// again in this turn.
	QuestionsClosed bool
	// QuestionsReopened: the user sent the plan back to questions; the agent
	// gets one more round.
	QuestionsReopened bool
}

func (s *Store) resolvePlanning(
	ctx context.Context, ownerUserID, sessionID, turnID, tool, eventType string,
	decide func(ctx context.Context, p PendingProposal) (*PlanningResolution, error),
) (*Turn, error) {
	return s.resolve(ctx, ownerUserID, sessionID, turnID,
		func(name string) bool { return name == tool },
		func(ctx context.Context, _ pgx.Tx, p PendingProposal) (*resolution, error) {
			res, err := decide(ctx, p)
			if err != nil {
				return nil, err
			}
			if res == nil {
				return nil, fmt.Errorf("%w: a resolution is required", ErrInvalidArgument)
			}
			return &resolution{
				ProposalResolution: ProposalResolution{Content: res.Content, EventType: eventType, EventPayload: res.EventPayload},
				questionsClosed:    res.QuestionsClosed,
				questionsReopened:  res.QuestionsReopened,
			}, nil
		})
}

// AnswerQuestions resolves a turn paused on ask_user: decide receives the
// questions that were asked and returns the result the agent reads. The turn
// goes back to pending, in the same mode. Errors as ResolveProposal;
// ErrProposalKind when the turn is paused on something else.
func (s *Store) AnswerQuestions(
	ctx context.Context, ownerUserID, sessionID, turnID string,
	decide func(ctx context.Context, p PendingProposal) (*PlanningResolution, error),
) (*Turn, error) {
	return s.resolvePlanning(ctx, ownerUserID, sessionID, turnID, PlanningToolAskUser, EventQuestionsAnswered, decide)
}

// ReviseBuildPlan resolves a turn paused on propose_build_plan without
// approving it: the agent reads the user's request and proposes again (or asks
// one more round, with QuestionsReopened). Nothing is stored as a brief.
// Errors as AnswerQuestions.
func (s *Store) ReviseBuildPlan(
	ctx context.Context, ownerUserID, sessionID, turnID string,
	decide func(ctx context.Context, p PendingProposal) (*PlanningResolution, error),
) (*Turn, error) {
	return s.resolvePlanning(ctx, ownerUserID, sessionID, turnID, PlanningToolProposeBuildPlan, EventPlanRevisionRequested, decide)
}

// PlanApproval is what the decide of ApproveBuildPlan returns.
type PlanApproval struct {
	// Plan is the normalized plan to store as the new brief version.
	Plan json.RawMessage
	// Milestones is the number of milestones of the plan.
	Milestones int
	// Milestone is the 1-based milestone the turn builds now (the first one
	// the plan does not mark done), or 0 when none is pending.
	Milestone int
	// Content returns the tool result text for the version the plan got.
	Content func(version int) string
}

// ApproveBuildPlan approves the plan a turn is paused on. In one transaction
// it stores the plan as the next brief version, moves the session to the
// building phase, makes the turn a build turn working on PlanApproval.Milestone
// of that version, and resumes it, so the build starts in the same turn. It
// returns the resumed turn and the new brief. Errors as AnswerQuestions.
func (s *Store) ApproveBuildPlan(
	ctx context.Context, ownerUserID, sessionID, turnID string,
	decide func(ctx context.Context, p PendingProposal) (*PlanApproval, error),
) (*Turn, *Brief, error) {
	var brief *Brief
	turn, err := s.resolve(ctx, ownerUserID, sessionID, turnID,
		func(name string) bool { return name == PlanningToolProposeBuildPlan },
		func(ctx context.Context, tx pgx.Tx, p PendingProposal) (*resolution, error) {
			a, err := decide(ctx, p)
			if err != nil {
				return nil, err
			}
			if a == nil || len(a.Plan) == 0 || a.Content == nil || a.Milestone < 0 || a.Milestone > a.Milestones {
				return nil, fmt.Errorf("%w: an approval with a plan and a result is required", ErrInvalidArgument)
			}
			// The turn row is locked by resolve, and a session has one active
			// turn, so two approvals cannot allocate the same version.
			b, err := scanBrief(tx.QueryRow(ctx,
				`INSERT INTO zeep_system.builder_briefs (session_id, version, plan, turn_id, created_by)
				 VALUES ($1, (SELECT COALESCE(MAX(version), 0) + 1 FROM zeep_system.builder_briefs WHERE session_id = $1),
				         $2::jsonb, $3, $4)
				 RETURNING `+briefColumns,
				sessionID, []byte(a.Plan), turnID, ownerUserID))
			if err != nil {
				return nil, fmt.Errorf("builder store: approve build plan: insert brief: %w", err)
			}
			var milestone *int
			if a.Milestone > 0 {
				milestone = &a.Milestone
			}
			if _, err := tx.Exec(ctx,
				`UPDATE zeep_system.builder_turns SET mode = 'build', brief_version = $2, milestone = $3 WHERE id = $1`,
				turnID, b.Version, milestone); err != nil {
				return nil, fmt.Errorf("builder store: approve build plan: turn: %w", err)
			}
			if _, err := tx.Exec(ctx,
				`UPDATE zeep_system.builder_sessions SET phase = 'building' WHERE id = $1`, sessionID); err != nil {
				return nil, fmt.Errorf("builder store: approve build plan: session: %w", err)
			}
			brief = b
			return &resolution{ProposalResolution: ProposalResolution{
				Content:      a.Content(b.Version),
				EventType:    EventPlanApproved,
				EventPayload: map[string]any{"tool_call_id": p.ToolCallID, "version": b.Version, "milestones": a.Milestones, "milestone": a.Milestone},
			}}, nil
		})
	if err != nil {
		return nil, nil, err
	}
	return turn, brief, nil
}
