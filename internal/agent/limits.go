package agent

import "time"

// Defaults match builder_settings (design, Data Models). They apply to any
// field left at zero in [Limits].
const (
	DefaultMaxSteps            = 40
	DefaultMaxFixIterations    = 3
	DefaultMaxTurnDuration     = 600 * time.Second
	DefaultContextWindowTokens = 128000
	DefaultCompactionThreshold = 0.8
	DefaultKeepRecentTurns     = 2
	DefaultMaxOutputTokens     = 0 // provider default

	// MaxSkillLoadsPerTurn is the number of distinct skills one turn may load.
	MaxSkillLoadsPerTurn = 5
)

// Limits bounds one agent turn. The dashboard fills it from builder_settings;
// zero values fall back to the Default* constants.
type Limits struct {
	// MaxSteps is the maximum number of model calls in one turn. A turn that
	// reaches it ends limit_reached (with a commit).
	MaxSteps int
	// MaxFixIterations is the maximum number of failed run_build calls in one
	// turn (design: "max_fix_iterations = failed run_build calls"). The turn
	// ends limit_reached as soon as the count reaches it, so the model gets
	// MaxFixIterations-1 chances to react to a failure after the first one.
	MaxFixIterations int
	// MaxTurnDuration bounds the wall-clock time of one RunTurn invocation. A
	// resumed turn gets a fresh allowance.
	MaxTurnDuration time.Duration
	// ContextWindowTokens is the model's context window, used by compaction.
	ContextWindowTokens int
	// CompactionThreshold is the fraction of the window above which older turns
	// are summarized (chars/4 heuristic).
	CompactionThreshold float64
	// KeepRecentTurns is how many of the most recent turns (the current one
	// included) stay verbatim when compacting.
	KeepRecentTurns int
	// MaxOutputTokens caps each model reply; 0 leaves the provider default.
	MaxOutputTokens int
}

// DefaultLimits returns the design defaults.
func DefaultLimits() Limits {
	return Limits{}.withDefaults()
}

func (l Limits) withDefaults() Limits {
	if l.MaxSteps <= 0 {
		l.MaxSteps = DefaultMaxSteps
	}
	if l.MaxFixIterations <= 0 {
		l.MaxFixIterations = DefaultMaxFixIterations
	}
	if l.MaxTurnDuration <= 0 {
		l.MaxTurnDuration = DefaultMaxTurnDuration
	}
	if l.ContextWindowTokens <= 0 {
		l.ContextWindowTokens = DefaultContextWindowTokens
	}
	if l.CompactionThreshold <= 0 || l.CompactionThreshold >= 1 {
		l.CompactionThreshold = DefaultCompactionThreshold
	}
	if l.KeepRecentTurns <= 0 {
		l.KeepRecentTurns = DefaultKeepRecentTurns
	}
	if l.MaxOutputTokens < 0 {
		l.MaxOutputTokens = DefaultMaxOutputTokens
	}
	return l
}

// Event types the loop appends (design, Data Models). Payloads never carry
// secrets, file contents or raw provider errors.
const (
	EventTurnStarted      = "turn_started"
	EventAssistantMessage = "assistant_message"
	EventToolCall         = "tool_call"
	EventFileChanged      = "file_changed"
	EventSkillLoaded      = "skill_loaded"
	EventBuildStarted     = "build_started"
	EventBuildSucceeded   = "build_succeeded"
	EventBuildFailed      = "build_failed"
	EventProposal         = "proposal"
	EventLimitReached     = "limit_reached"
	EventBudgetExceeded   = "budget_exceeded"
	EventError            = "error"
	EventTurnFinished     = "turn_finished"
)

// Generic error codes stored in builder_turns.error_code and in `error`
// events. They are the only failure information users see.
const (
	CodeProviderUnavailable = "provider_unavailable"
	CodeContextTooLong      = "context_too_long"
	CodeWorkspaceLost       = "workspace_lost"
	CodeToolFailed          = "tool_failed"
	CodeCommitFailed        = "commit_failed"
	CodeInternal            = "internal_error"
	CodeUnsupportedTurn     = "unsupported_turn_kind"
)

// Limit names carried in `limit_reached` events.
const (
	LimitSteps    = "max_steps"
	LimitFixes    = "max_fix_iterations"
	LimitDuration = "max_turn_duration"
)
