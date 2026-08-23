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

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard/ai"
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
