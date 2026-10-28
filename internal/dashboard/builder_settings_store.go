package dashboard

// builder_settings_store.go — the AI Frontend Builder's global settings (the
// singleton zeep_system.builder_settings row) and the versioned company
// persona + rules (zeep_system.builder_prompt_versions) — ai-frontend-builder
// T22, AFB-01/02/25.
//
// Settings semantics:
//   - The row is created lazily by the first PUT. Until then GetBuilderSettings
//     returns the column defaults (disabled, caps 0) WITHOUT writing: the
//     budget checker (internal/agent/store/usage.go) treats a missing row as
//     "not configured", i.e. sessions blocked, which is exactly what a fresh
//     install should be.
//   - Updates are merge-on-absent-key (the mergeProviderConfig convention): a
//     nil field keeps the stored value, so a form that only changes the caps
//     cannot silently reset the other settings. To zero a cap the client sends
//     0 explicitly.
//
// Prompt semantics: append-only history; the active version is the newest row.
// A save never edits a row, so "previous version kept" holds by construction.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Defaults of a never-saved settings row. They mirror the column defaults in
// provisioner.go (builder_settings) and internal/agent/limits.go.
const (
	builderDefaultProvider            = aiProviderOpenAI
	builderDefaultContextWindowTokens = 128000
	builderDefaultMaxStepsPerTurn     = 40
	builderDefaultMaxFixIterations    = 3
	builderDefaultMaxTurnSeconds      = 600
	builderDefaultApprovalMode        = BuilderApprovalAsk
	builderDefaultPublishRLSGate      = BuilderRLSGateWarn
)

// Backend approval modes (builder_settings.backend_approval_mode) and publish
// checks (builder_settings.publish_rls_gate), ai-builder-backend spec BBE-15.
const (
	BuilderApprovalAsk  = "ask"
	BuilderApprovalAuto = "auto"
	BuilderRLSGateWarn  = "warn"
	BuilderRLSGateBlock = "block"
)

// BuilderSettings is the API view of the singleton settings row.
type BuilderSettings struct {
	Enabled             bool   `json:"enabled"`
	Provider            string `json:"provider"`
	ContextWindowTokens int    `json:"context_window_tokens"`
	// SessionTokenCap / UserMonthlyTokenCap: tokens (in + out). 0 means unset:
	// builder sessions stay blocked until both are positive (AFB-02, AFB-14).
	SessionTokenCap     int64 `json:"session_token_cap"`
	UserMonthlyTokenCap int64 `json:"user_monthly_token_cap"`
	MaxStepsPerTurn     int   `json:"max_steps_per_turn"`
	MaxFixIterations    int   `json:"max_fix_iterations"`
	MaxTurnSeconds      int   `json:"max_turn_seconds"`
	// BackendApprovalMode: "ask" (every backend change waits for the user) or
	// "auto" (additive changes on a builder-created backend apply directly).
	BackendApprovalMode string `json:"backend_approval_mode"`
	// PublishRLSGate: "warn" lists open tables on publish, "block" refuses.
	PublishRLSGate string `json:"publish_rls_gate"`
	// UpdatedBy and UpdatedAt are nil/zero while the row has never been saved.
	UpdatedBy *string    `json:"updated_by"`
	UpdatedAt *time.Time `json:"updated_at"`
	// Configured is false while the row has never been saved (defaults shown).
	Configured bool `json:"configured"`
}

// BuilderSettingsInput is the merge-on-absent-key body of PUT
// /api/builder/settings: nil means "keep the stored value".
type BuilderSettingsInput struct {
	Enabled             *bool   `json:"enabled"`
	Provider            *string `json:"provider"`
	ContextWindowTokens *int    `json:"context_window_tokens"`
	SessionTokenCap     *int64  `json:"session_token_cap"`
	UserMonthlyTokenCap *int64  `json:"user_monthly_token_cap"`
	MaxStepsPerTurn     *int    `json:"max_steps_per_turn"`
	MaxFixIterations    *int    `json:"max_fix_iterations"`
	MaxTurnSeconds      *int    `json:"max_turn_seconds"`
	BackendApprovalMode *string `json:"backend_approval_mode"`
	PublishRLSGate      *string `json:"publish_rls_gate"`
}

func defaultBuilderSettings() *BuilderSettings {
	return &BuilderSettings{
		Provider:            builderDefaultProvider,
		ContextWindowTokens: builderDefaultContextWindowTokens,
		MaxStepsPerTurn:     builderDefaultMaxStepsPerTurn,
		MaxFixIterations:    builderDefaultMaxFixIterations,
		MaxTurnSeconds:      builderDefaultMaxTurnSeconds,
		BackendApprovalMode: builderDefaultApprovalMode,
		PublishRLSGate:      builderDefaultPublishRLSGate,
	}
}

const builderSettingsCols = `enabled, provider, context_window_tokens, session_token_cap, user_monthly_token_cap,
	max_steps_per_turn, max_fix_iterations, max_turn_seconds, backend_approval_mode, publish_rls_gate,
	updated_by::text, updated_at`

func scanBuilderSettings(row pgx.Row) (*BuilderSettings, error) {
	var (
		s         BuilderSettings
		updatedBy *string
		updatedAt time.Time
	)
	if err := row.Scan(&s.Enabled, &s.Provider, &s.ContextWindowTokens, &s.SessionTokenCap, &s.UserMonthlyTokenCap,
		&s.MaxStepsPerTurn, &s.MaxFixIterations, &s.MaxTurnSeconds, &s.BackendApprovalMode, &s.PublishRLSGate,
		&updatedBy, &updatedAt); err != nil {
		return nil, err
	}
	s.UpdatedBy = updatedBy
	s.UpdatedAt = &updatedAt
	s.Configured = true
	return &s, nil
}

// GetBuilderSettings returns the settings row, or the defaults (not written)
// when it does not exist yet.
func GetBuilderSettings(ctx context.Context, pool *db.Pool) (*BuilderSettings, error) {
	s, err := scanBuilderSettings(pool.QueryRow(ctx,
		`SELECT `+builderSettingsCols+` FROM zeep_system.builder_settings WHERE id = 1`))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return defaultBuilderSettings(), nil
		}
		return nil, fmt.Errorf("dashboard: get builder settings: %w", err)
	}
	return s, nil
}

// UpsertBuilderSettings creates the singleton row on the first call and
// merges in.* into it afterward (nil = keep), atomically. updatedBy is the
// acting user's id.
func UpsertBuilderSettings(ctx context.Context, pool *db.Pool, updatedBy string, in BuilderSettingsInput) (*BuilderSettings, error) {
	s, err := scanBuilderSettings(pool.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_settings
		   (id, enabled, provider, context_window_tokens, session_token_cap, user_monthly_token_cap,
		    max_steps_per_turn, max_fix_iterations, max_turn_seconds,
		    backend_approval_mode, publish_rls_gate, updated_by, updated_at)
		 VALUES (1,
		   COALESCE($1::boolean, false), COALESCE($2::text, $9), COALESCE($3::int, $10),
		   COALESCE($4::bigint, 0), COALESCE($5::bigint, 0),
		   COALESCE($6::int, $11), COALESCE($7::int, $12), COALESCE($8::int, $13),
		   COALESCE($15::text, $17), COALESCE($16::text, $18),
		   $14::uuid, now())
		 ON CONFLICT (id) DO UPDATE SET
		   enabled                = COALESCE($1::boolean, builder_settings.enabled),
		   provider               = COALESCE($2::text,    builder_settings.provider),
		   context_window_tokens  = COALESCE($3::int,     builder_settings.context_window_tokens),
		   session_token_cap      = COALESCE($4::bigint,  builder_settings.session_token_cap),
		   user_monthly_token_cap = COALESCE($5::bigint,  builder_settings.user_monthly_token_cap),
		   max_steps_per_turn     = COALESCE($6::int,     builder_settings.max_steps_per_turn),
		   max_fix_iterations     = COALESCE($7::int,     builder_settings.max_fix_iterations),
		   max_turn_seconds       = COALESCE($8::int,     builder_settings.max_turn_seconds),
		   backend_approval_mode  = COALESCE($15::text,   builder_settings.backend_approval_mode),
		   publish_rls_gate       = COALESCE($16::text,   builder_settings.publish_rls_gate),
		   updated_by             = $14::uuid,
		   updated_at             = now()
		 RETURNING `+builderSettingsCols,
		in.Enabled, in.Provider, in.ContextWindowTokens, in.SessionTokenCap, in.UserMonthlyTokenCap,
		in.MaxStepsPerTurn, in.MaxFixIterations, in.MaxTurnSeconds,
		builderDefaultProvider, builderDefaultContextWindowTokens,
		builderDefaultMaxStepsPerTurn, builderDefaultMaxFixIterations, builderDefaultMaxTurnSeconds,
		updatedBy,
		in.BackendApprovalMode, in.PublishRLSGate, builderDefaultApprovalMode, builderDefaultPublishRLSGate,
	))
	if err != nil {
		return nil, fmt.Errorf("dashboard: upsert builder settings: %w", err)
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Persona + rules (versioned)
// ---------------------------------------------------------------------------

// BuilderPromptMaxChars is the save-time cap on the company layer, counted on
// the text the prompt will actually contain (agent.CompanyLayer: trimmed
// persona + "Rules:" header + trimmed rules). It is the same bound
// agent.BuildSystemPrompt applies defensively at assembly time, so a saved
// version is never truncated.
const BuilderPromptMaxChars = agent.MaxCompanyChars

// ErrBuilderPromptTooLong is returned by validateBuilderPrompt when persona +
// rules exceed BuilderPromptMaxChars.
var ErrBuilderPromptTooLong = errors.New("persona and rules exceed the maximum size")

// errBuilderPromptInvalid is returned by validateBuilderPrompt for content that
// cannot be stored (a NUL byte, which Postgres TEXT rejects, or invalid UTF-8).
var errBuilderPromptInvalid = errors.New("persona and rules must be valid text without NUL characters")

// validateBuilderPrompt checks the merged persona + rules before saving.
func validateBuilderPrompt(persona, rules string) error {
	for _, s := range []string{persona, rules} {
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return errBuilderPromptInvalid
		}
	}
	if n := builderPromptChars(persona, rules); n > BuilderPromptMaxChars {
		return fmt.Errorf("%w: %d characters, the limit is %d (persona and rules combined)", ErrBuilderPromptTooLong, n, BuilderPromptMaxChars)
	}
	return nil
}

// BuilderPromptVersion is one row of builder_prompt_versions.
type BuilderPromptVersion struct {
	ID             string    `json:"id"`
	Persona        string    `json:"persona"`
	Rules          string    `json:"rules"`
	CreatedBy      string    `json:"created_by"`
	CreatedByEmail string    `json:"created_by_email"`
	CreatedAt      time.Time `json:"created_at"`
}

// BuilderPromptInput is the merge-on-absent-key body of PUT /api/builder/prompt.
// nil keeps the active version's value for that field; an explicit "" clears it.
type BuilderPromptInput struct {
	Persona *string `json:"persona"`
	Rules   *string `json:"rules"`
}

// builderPromptChars is the size of the company layer the prompt would carry
// for persona+rules, in characters (runes).
func builderPromptChars(persona, rules string) int {
	return utf8.RuneCountInString(agent.CompanyLayer(persona, rules))
}

const builderPromptCols = `v.id::text, v.persona, v.rules, v.created_by::text, COALESCE(u.email, ''), v.created_at`

const builderPromptFrom = ` FROM zeep_system.builder_prompt_versions v
	LEFT JOIN zeep_system.dashboard_users u ON u.id = v.created_by`

func scanBuilderPromptVersion(row pgx.Row) (*BuilderPromptVersion, error) {
	var v BuilderPromptVersion
	if err := row.Scan(&v.ID, &v.Persona, &v.Rules, &v.CreatedBy, &v.CreatedByEmail, &v.CreatedAt); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetActiveBuilderPrompt returns the newest version, or nil (not an error)
// when none has been saved yet.
func GetActiveBuilderPrompt(ctx context.Context, pool db.Querier) (*BuilderPromptVersion, error) {
	v, err := scanBuilderPromptVersion(pool.QueryRow(ctx,
		`SELECT `+builderPromptCols+builderPromptFrom+` ORDER BY v.created_at DESC, v.id DESC LIMIT 1`))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: get active builder prompt: %w", err)
	}
	return v, nil
}

// GetBuilderPromptVersion returns the version with the given id, or nil (not an
// error) when it does not exist or id is not a UUID. A session pins its prompt
// version at creation (sessions.prompt_version_id), so the agent loads that row
// rather than the active one.
func GetBuilderPromptVersion(ctx context.Context, pool db.Querier, id string) (*BuilderPromptVersion, error) {
	if !looksLikeUUID(id) {
		return nil, nil
	}
	v, err := scanBuilderPromptVersion(pool.QueryRow(ctx,
		`SELECT `+builderPromptCols+builderPromptFrom+` WHERE v.id = $1::uuid`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: get builder prompt version: %w", err)
	}
	return v, nil
}

// ListBuilderPromptVersions returns versions newest first.
func ListBuilderPromptVersions(ctx context.Context, pool *db.Pool, limit, offset int) ([]BuilderPromptVersion, int, error) {
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM zeep_system.builder_prompt_versions`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("dashboard: count builder prompt versions: %w", err)
	}
	rows, err := pool.Query(ctx,
		`SELECT `+builderPromptCols+builderPromptFrom+` ORDER BY v.created_at DESC, v.id DESC LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("dashboard: list builder prompt versions: %w", err)
	}
	defer rows.Close()

	out := make([]BuilderPromptVersion, 0)
	for rows.Next() {
		v, err := scanBuilderPromptVersion(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("dashboard: list builder prompt versions scan: %w", err)
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("dashboard: list builder prompt versions rows: %w", err)
	}
	return out, total, nil
}

// SaveBuilderPrompt appends a new version built from the active one merged
// with in (nil fields keep the active value), and returns it with
// created=true. When the merged content equals the active version nothing is
// written: it returns the active version with created=false (a double click
// on "save" must not pollute the history). The size cap is validated here too
// (ErrBuilderPromptTooLong), so no caller can bypass it.
//
// The read-merge-insert runs in one transaction behind an advisory lock, so
// two concurrent partial saves cannot lose each other's field.
func SaveBuilderPrompt(ctx context.Context, pool *db.Pool, authorID string, in BuilderPromptInput) (v *BuilderPromptVersion, created bool, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("dashboard: save builder prompt: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('zeep_system.builder_prompt_versions'))`); err != nil {
		return nil, false, fmt.Errorf("dashboard: save builder prompt: lock: %w", err)
	}

	active, err := GetActiveBuilderPrompt(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	persona, rules := "", ""
	if active != nil {
		persona, rules = active.Persona, active.Rules
	}
	if in.Persona != nil {
		persona = *in.Persona
	}
	if in.Rules != nil {
		rules = *in.Rules
	}
	if err := validateBuilderPrompt(persona, rules); err != nil {
		return nil, false, err
	}
	if active != nil && active.Persona == persona && active.Rules == rules {
		return active, false, nil
	}

	// clock_timestamp(), not now(): now() is the transaction START time, so a
	// transaction that began earlier but took the lock later would insert an
	// older created_at than the version it follows and break "newest = active".
	var id string
	if err := tx.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_prompt_versions (persona, rules, created_by, created_at)
		 VALUES ($1, $2, $3::uuid, clock_timestamp())
		 RETURNING id::text`,
		persona, rules, authorID,
	).Scan(&id); err != nil {
		return nil, false, fmt.Errorf("dashboard: save builder prompt: insert: %w", err)
	}
	v, err = scanBuilderPromptVersion(tx.QueryRow(ctx,
		`SELECT `+builderPromptCols+builderPromptFrom+` WHERE v.id = $1::uuid`, id))
	if err != nil {
		return nil, false, fmt.Errorf("dashboard: save builder prompt: reload: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("dashboard: save builder prompt: commit: %w", err)
	}
	return v, true, nil
}
