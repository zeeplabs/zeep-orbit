package dashboard

// ai_build_chat_handlers.go — HTTP handlers for the "Build with AI" chat:
// turn-taking (BuildChatTurn), session resume (GetBuildChatSession), plan
// confirmation (BuildChatConfirm, T9), and restart (RestartBuildChatSession,
// T10). Orchestrates the session store (ai_build_sessions_store.go), the
// provider store (ai_providers_store.go), and the OpenAI client
// (internal/dashboard/ai). See .specs/features/ai-build-chat/design.md's
// Components section for the BuildChatTurn/BuildChatConfirm contract.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard/ai"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// buildChatSystemPrompt is the fixed system message prepended to every
// OpenAI call for a "Build with AI" session — steers the model toward
// clarifying questions, existing-app lookups via the read-only tools, and
// calling propose_app_plan only once it has enough information (spec.md
// P3).
const buildChatSystemPrompt = "You are an assistant embedded in zeep-orbit's dashboard that helps a user describe a new backend app in plain language. Ask clarifying questions (e.g. whether login/auth is needed, what data it stores) until you have enough information, then call propose_app_plan with a concrete name, tables, and columns. If the user references an existing app, use list_apps and get_app_schema to look up its real schema instead of guessing. Never invent schema details for an app you have not looked up."

// genericAIChatError is the fixed chat-visible message shown whenever the
// OpenAI call fails or the provider is unavailable/misconfigured — the real
// error is logged server-side only, never returned to the caller (AGENTS.md
// §4, spec AIBC-16).
const genericAIChatError = "couldn't generate a plan right now, try again"

// aiProviderName is the only provider the chat call path resolves against
// today (gemini/claude have no functional call path — spec Out of Scope).
const aiProviderName = "openai"

// callAIModel is a package-level indirection over ai.CallModel so tests in
// this package can substitute a fake model response (message/plan/error)
// without hitting the real OpenAI API or reaching into internal/dashboard/ai's
// own private test seam (chatCompletionsURL is unexported there, by design).
var callAIModel = ai.CallModel

type buildChatTurnRequest struct {
	Content string `json:"content"`
}

// buildChatTurnResponse is exactly one of {type: "message", content} or
// {type: "plan", plan}, never both — spec.md's function-calling assumption.
type buildChatTurnResponse struct {
	Type    string      `json:"type"`
	Content string      `json:"content,omitempty"`
	Plan    *ai.AppPlan `json:"plan,omitempty"`
}

type buildChatSessionResponse struct {
	Session  *AIBuildSession  `json:"session"`
	Messages []AIBuildMessage `json:"messages"`
}

// GetBuildChatSession handles GET /dashboard/api/ai/build-chat/session —
// resumes the caller's in_progress session with its full history (AIBC-07),
// or creates a fresh one if none exists (AIBC-08).
func (h *Handler) GetBuildChatSession(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	session, messages, err := GetOrCreateInProgressSession(r.Context(), h.pool, user.ID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, buildChatSessionResponse{Session: session, Messages: messages})
}

// BuildChatTurn handles POST /dashboard/api/ai/build-chat. It persists the
// user's message, calls the configured OpenAI model with the session's full
// history, persists the assistant's response, and returns exactly one of
// {type: "message"} or {type: "plan"} (AIBC-12/13/14).
func (h *Handler) BuildChatTurn(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var body buildChatTurnRequest
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	session, history, err := GetOrCreateInProgressSession(r.Context(), h.pool, user.ID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if err := AppendMessage(r.Context(), h.pool, session.ID, "user", body.Content, nil); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	model, apiKey, err := resolveDecryptedAIProviderKey(r.Context(), h.pool, aiProviderName)
	if err != nil {
		// Unconfigured/disabled/undecryptable provider — treated identically
		// to a provider call failure from the user's point of view (spec
		// Edge Cases), logged server-side for a superadmin to investigate.
		h.logger.Warn("ai build chat: provider unavailable", zap.String("session_id", session.ID), zap.Error(err))
		writeJSON(w, http.StatusOK, buildChatTurnResponse{Type: "message", Content: genericAIChatError})
		return
	}

	messages := make([]ai.Message, 0, len(history)+2)
	messages = append(messages, ai.Message{Role: "system", Content: buildChatSystemPrompt})
	for _, m := range history {
		messages = append(messages, ai.Message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, ai.Message{Role: "user", Content: body.Content})

	result, err := callAIModel(r.Context(), model, apiKey, messages, h.buildChatReadToolInvoker(user))
	if err != nil {
		// AIBC-16: generic chat-visible message, real error logged server-side
		// only — never leaked to the caller.
		h.logger.Error("ai build chat: model call failed", zap.String("session_id", session.ID), zap.Error(err))
		writeJSON(w, http.StatusOK, buildChatTurnResponse{Type: "message", Content: genericAIChatError})
		return
	}

	if result.Kind == "plan" && result.Plan != nil {
		planJSON, err := json.Marshal(result.Plan)
		if err != nil {
			h.logger.Error("ai build chat: marshal plan failed", zap.String("session_id", session.ID), zap.Error(err))
			writeJSON(w, http.StatusOK, buildChatTurnResponse{Type: "message", Content: genericAIChatError})
			return
		}
		if err := AppendMessage(r.Context(), h.pool, session.ID, "assistant", "", planJSON); err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
			return
		}
		writeJSON(w, http.StatusOK, buildChatTurnResponse{Type: "plan", Plan: result.Plan})
		return
	}

	if err := AppendMessage(r.Context(), h.pool, session.ID, "assistant", result.Content, nil); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, buildChatTurnResponse{Type: "message", Content: result.Content})
}

// buildChatReadToolInvoker closes over the authenticated user (and h.pool)
// so internal/dashboard/ai never imports the dashboard package directly
// (design.md's ReadToolInvoker contract) — it resolves list_apps/
// get_app_schema tool calls against ListAppsForUser/GetAppSchemaForUser,
// the exact same read paths orbit_list_apps/orbit_get_app_schema use
// (AIBC-17), never fabricating schema for an app it hasn't looked up.
func (h *Handler) buildChatReadToolInvoker(user *DashboardUser) ai.ReadToolInvoker {
	return func(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
		switch name {
		case "list_apps":
			apps, err := ListAppsForUser(ctx, h.pool, user)
			if err != nil {
				return nil, fmt.Errorf("dashboard: list_apps read tool: %w", err)
			}
			return json.Marshal(apps)

		case "get_app_schema":
			var in struct {
				AppName string `json:"app_name"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("dashboard: parse get_app_schema arguments: %w", err)
			}

			apps, err := ListAppsForUser(ctx, h.pool, user)
			if err != nil {
				return nil, fmt.Errorf("dashboard: get_app_schema read tool: %w", err)
			}
			var appID string
			for _, a := range apps {
				if a.Name == in.AppName {
					appID = a.ID
					break
				}
			}
			if appID == "" {
				return json.Marshal(map[string]string{"error": "app not found: " + in.AppName})
			}

			schema, err := GetAppSchemaForUser(ctx, h.pool, user, appID)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					return json.Marshal(map[string]string{"error": "app not found: " + in.AppName})
				}
				return nil, fmt.Errorf("dashboard: get_app_schema read tool: %w", err)
			}
			return json.Marshal(schema)

		default:
			return nil, fmt.Errorf("dashboard: unknown read tool %q", name)
		}
	}
}

// buildChatConfirmResponse is BuildChatConfirm's success payload — the
// fully created (or already-existing, on a successful retry) app.
type buildChatConfirmResponse struct {
	App *AppRow `json:"app"`
}

// loadOwnedBuildChatSession loads sessionID's row and full message history,
// scoped to ownerUserID (AIBC-11) so one user can never confirm — or even
// see — another user's session. Reuses listMessages, the package-private
// helper ai_build_sessions_store.go (T6) already defines, instead of
// duplicating its query.
func loadOwnedBuildChatSession(ctx context.Context, pool *db.Pool, sessionID, ownerUserID string) (*AIBuildSession, []AIBuildMessage, error) {
	var s AIBuildSession
	err := pool.QueryRow(ctx,
		`SELECT id, owner_user_id, status, created_app_id, created_at, updated_at
		 FROM zeep_system.ai_build_sessions
		 WHERE id = $1 AND owner_user_id = $2`,
		sessionID, ownerUserID,
	).Scan(&s.ID, &s.OwnerUserID, &s.Status, &s.CreatedAppID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("dashboard: load owned ai build session: %w", err)
	}

	messages, err := listMessages(ctx, pool, s.ID)
	if err != nil {
		return nil, nil, err
	}
	return &s, messages, nil
}

// BuildChatConfirm handles POST /dashboard/api/ai/build-chat/{session_id}/confirm.
//
// SPEC_DEVIATION: this handler reads no plan from the request body at all —
// the plan it executes is always the one BuildChatTurn (T8) already
// persisted on the session's latest assistant message. design.md's Confirm
// section doesn't specify where the plan comes from at request time; reading
// the request body was one option, but AIBC-24 ("SHALL NOT accept a
// free-form/unstructured plan payload... only the structured shape produced
// by propose_app_plan") is satisfied more strongly by never trusting any
// client-supplied plan JSON in the first place, closing the prompt-
// injection-into-schema risk spec.md's function-calling assumption exists to
// prevent. Reason: a body-accepting confirm would still need this exact
// "does it match the last propose_app_plan call" check to satisfy AIBC-24,
// so skipping the body entirely is simpler and strictly safer, not a lesser
// implementation of the same contract.
//
// It never trusts a client-supplied plan payload (AIBC-24): the plan is
// always the one the model itself proposed and the server already persisted
// on the session's latest assistant message (propose_app_plan's structured
// output, T8). It validates every table name before any mutation runs
// (rejecting a plan with a reserved/invalid name up front), then calls
// CreateAppForUser once and CreateAppTableForUser per table — re-checking
// GetApp fresh before each table attempt so a retry after a partial failure
// skips tables that already exist instead of erroring as a duplicate
// (AIBC-19 through AIBC-24).
func (h *Handler) BuildChatConfirm(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	sessionID := chi.URLParam(r, "session_id")

	session, messages, err := loadOwnedBuildChatSession(r.Context(), h.pool, sessionID, user.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if session.Status != "in_progress" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session is not in progress"})
		return
	}

	plan := latestProposedPlan(messages)
	if plan == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no proposed plan to confirm"})
		return
	}
	if err := validatePlanTableNames(plan); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var app *AppRow
	if session.CreatedAppID == nil || *session.CreatedAppID == "" {
		created, err := h.CreateAppForUser(r.Context(), user, AppRequestBody{
			Name:             plan.Name,
			AuthEmailEnabled: plan.Auth,
		}, "ai_chat")
		if err != nil {
			h.respondBuildChatConfirmError(w, r, err)
			return
		}
		if err := SetSessionCreatedApp(r.Context(), h.pool, session.ID, created.ID); err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
			return
		}
		app = created
	} else {
		existing, _, err := GetApp(r.Context(), h.pool, *session.CreatedAppID, user)
		if err != nil {
			h.respondBuildChatConfirmError(w, r, err)
			return
		}
		app = existing
	}

	for _, table := range plan.Tables {
		fresh, _, err := GetApp(r.Context(), h.pool, app.ID, user)
		if err != nil {
			h.respondBuildChatConfirmError(w, r, err)
			return
		}
		if appTableRowExists(fresh.Tables, table.Name) {
			continue
		}

		_, err = h.CreateAppTableForUser(r.Context(), user, app.ID, TableRequestBody{
			Name:    table.Name,
			Columns: planColumnsToConfig(table.Columns),
		}, "ai_chat")
		if err != nil {
			var valErr *ValidationError
			if errors.As(err, &valErr) && strings.Contains(valErr.Error(), "duplicate table name") {
				// A previous attempt already created this table — a
				// concurrent retry raced past the fresh GetApp check above.
				// Treat as a successful no-op, not a failure (AIBC-23).
				continue
			}
			h.respondBuildChatConfirmError(w, r, err)
			return
		}
	}

	if err := CompleteSession(r.Context(), h.pool, session.ID, app.ID); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	final, _, err := GetApp(r.Context(), h.pool, app.ID, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	final.RedactSecrets()
	writeJSON(w, http.StatusOK, buildChatConfirmResponse{App: final})
}

// respondBuildChatConfirmError maps an error from CreateAppForUser/
// CreateAppTableForUser/GetApp to the same HTTP status the manual REST
// create-app/create-table handlers already use for each error class
// (CreateApp/CreateAppTable in handler.go) — a partial failure here leaves
// the session in_progress (the caller already recorded created_app_id, if
// any, before this point), never rolling back what succeeded (AIBC-22).
func (h *Handler) respondBuildChatConfirmError(w http.ResponseWriter, r *http.Request, err error) {
	var valErr *ValidationError
	var typeErr *provisioner.TypeChangeError
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.As(err, &valErr):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": valErr.Error()})
	case errors.As(err, &typeErr):
		h.writeError(w, r, http.StatusBadRequest, typeErr.Error(), err)
	default:
		h.writeError(w, r, http.StatusInternalServerError, genericAIChatError, err)
	}
}

// latestProposedPlan returns the most recent assistant message's plan, or
// nil if the session has no proposed plan yet.
func latestProposedPlan(messages []AIBuildMessage) *ai.AppPlan {
	for i := len(messages) - 1; i >= 0; i-- {
		if len(messages[i].Plan) == 0 {
			continue
		}
		var plan ai.AppPlan
		if err := json.Unmarshal(messages[i].Plan, &plan); err != nil {
			continue
		}
		return &plan
	}
	return nil
}

// validatePlanTableNames rejects a plan containing any table name that
// doesn't match the same identifier rule CreateAppTableForUser enforces
// (identRe, handler.go) — including reserved-looking names like
// "_auth_users" which fail it by starting with an underscore — before any
// provisioner or CreateAppForUser call runs (spec Edge Cases).
func validatePlanTableNames(plan *ai.AppPlan) error {
	for _, t := range plan.Tables {
		if !identRe.MatchString(t.Name) {
			return &ValidationError{msg: "invalid table name: " + t.Name}
		}
	}
	return nil
}

// appTableRowExists reports whether name is already present in tables — the
// idempotent-retry skip check, evaluated against a freshly fetched app on
// every table attempt (never a stale in-memory list) so a table that
// provisioned but failed to persist its metadata row on a prior attempt is
// retried, not skipped (design.md Risks & Concerns).
func appTableRowExists(tables []AppTableRow, name string) bool {
	for _, t := range tables {
		if t.Name == name {
			return true
		}
	}
	return false
}

// planColumnsToConfig maps the AI-proposed plan's simplified {name, type}
// columns onto config.ColumnConfig — only Name/Type are populated; the plan
// has no notion of Required/Default/Unique/References (spec's
// propose_app_plan tool schema), so those stay at their zero values.
func planColumnsToConfig(cols []ai.PlanColumn) []config.ColumnConfig {
	out := make([]config.ColumnConfig, 0, len(cols))
	for _, c := range cols {
		out = append(out, config.ColumnConfig{Name: c.Name, Type: c.Type})
	}
	return out
}

// RestartBuildChatSession handles POST /dashboard/api/ai/build-chat/restart.
// A thin wrapper over AbandonAndRestartSession (T6): marks the caller's
// current in_progress session (if any) abandoned — preserving its messages
// — and returns a fresh in_progress session with an empty history (AIBC-09).
func (h *Handler) RestartBuildChatSession(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	session, err := AbandonAndRestartSession(r.Context(), h.pool, user.ID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, buildChatSessionResponse{Session: session, Messages: []AIBuildMessage{}})
}
