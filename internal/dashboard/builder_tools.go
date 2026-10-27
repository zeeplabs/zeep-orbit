package dashboard

// builder_tools.go registers the builder agent's Orbit-facing tools
// (ai-frontend-builder T16, AFB-11 / AFB-12): two read tools scoped to the
// session's backend app and the propose_backend_change proposal tool.
//
// The dashboard imports internal/agent/tools and internal/agent/store, never
// the reverse, so the tools package stays free of Orbit business logic.
//
// Scoping rules:
//   - The backend app id always comes from the session row. The model's
//     arguments are never consulted for the read tools (no app id in the
//     schema; anything the model sends is ignored), so a model cannot read
//     another app by guessing an id or name.
//   - Access is evaluated with the permissions of the session owner, through
//     the same *ForUser read paths the dashboard and MCP server use. If the
//     owner lost access to the app (or was deleted) the tool fails with a tool
//     error the model can relay; nothing about the app is returned.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard/ai"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Names of the builder's Orbit tools.
const (
	ToolOrbitGetAppSchema      = "orbit_get_app_schema"
	ToolOrbitListAuthProviders = "orbit_list_auth_providers"
	ToolProposeBackendChange   = "propose_backend_change"
)

// builderAppUnavailableMsg is the tool error shown to the model when the
// session owner can no longer see the backend app. It is deliberately the
// same text for "app gone" and "owner lost access" so it does not reveal which.
const builderAppUnavailableMsg = "the backend app is not available: the session owner no longer has access to it"

// BuilderSessionReader is the part of the builder store the tools need.
// *store.Store satisfies it.
type BuilderSessionReader interface {
	GetSessionByID(ctx context.Context, sessionID string) (*store.Session, error)
}

// BuilderToolDeps are the dependencies of the Orbit builder tools.
type BuilderToolDeps struct {
	Pool     *db.Pool
	Sessions BuilderSessionReader
}

// RegisterBuilderTools registers orbit_get_app_schema,
// orbit_list_auth_providers and propose_backend_change on reg.
func RegisterBuilderTools(reg *tools.Registry, deps BuilderToolDeps) error {
	if reg == nil {
		return errors.New("dashboard: builder tools: registry is required")
	}
	if deps.Pool == nil || deps.Sessions == nil {
		return errors.New("dashboard: builder tools: pool and session reader are required")
	}
	bt := &builderTools{deps: deps}
	for _, t := range bt.tools() {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

type builderTools struct {
	deps BuilderToolDeps
}

const noArgsSchema = `{"type":"object","properties":{},"additionalProperties":false}`

func (b *builderTools) tools() []tools.Tool {
	return []tools.Tool{
		{
			Name:        ToolOrbitGetAppSchema,
			Description: "Get the current schema of this project's backend app: tables, columns (name, type, nullable), row-level-security mode and policies. Takes no arguments; it always reads this project's own backend. Call it before writing code that reads or writes data.",
			Schema:      json.RawMessage(noArgsSchema),
			Kind:        tools.KindReadOnly,
			Handler:     b.getAppSchema,
		},
		{
			Name:        ToolOrbitListAuthProviders,
			Description: "List the login providers configured on this project's backend app (client secrets are never returned). Takes no arguments; it always reads this project's own backend.",
			Schema:      json.RawMessage(noArgsSchema),
			Kind:        tools.KindReadOnly,
			Handler:     b.listAuthProviders,
		},
		{
			Name:        ToolProposeBackendChange,
			Description: "Propose one change to this project's backend. It does NOT apply the change: the turn pauses until the user confirms or rejects it in the dashboard. Put exactly one operation in \"operation\": its \"kind\" plus the payload under the key with the same name as the kind.",
			Schema:      json.RawMessage(proposeBackendChangeSchema),
			Kind:        tools.KindProposal,
			Handler:     b.proposeBackendChange,
		},
	}
}

// ownerAndApp resolves the session's owner (as a DashboardUser) and fixed
// backend app id. A missing session or a database failure is a plain Go error
// (server-side only); a missing owner or app id is the model-visible
// "unavailable" tool error.
func (b *builderTools) ownerAndApp(ctx context.Context, sessionID string) (*DashboardUser, string, error) {
	sess, err := b.deps.Sessions.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, "", fmt.Errorf("dashboard: builder tools: load session: %w", err)
	}
	if sess.BackendAppID == "" {
		return nil, "", tools.ToolErrorf("%s", builderAppUnavailableMsg)
	}
	owner, err := GetUser(ctx, b.deps.Pool, sess.OwnerUserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, "", tools.ToolErrorf("%s", builderAppUnavailableMsg)
		}
		return nil, "", fmt.Errorf("dashboard: builder tools: load session owner: %w", err)
	}
	return owner, sess.BackendAppID, nil
}

func (b *builderTools) getAppSchema(ctx context.Context, call tools.Call) (tools.Result, error) {
	// call.Arguments is intentionally not read: the app is fixed by the session.
	owner, appID, err := b.ownerAndApp(ctx, call.SessionID)
	if err != nil {
		return tools.Result{}, err
	}
	schema, err := GetAppSchemaForUser(ctx, b.deps.Pool, owner, appID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return tools.Result{}, tools.ToolErrorf("%s", builderAppUnavailableMsg)
		}
		return tools.Result{}, fmt.Errorf("dashboard: builder tools: get app schema: %w", err)
	}
	out, err := json.Marshal(schema)
	if err != nil {
		return tools.Result{}, fmt.Errorf("dashboard: builder tools: marshal schema: %w", err)
	}
	return tools.Result{Content: string(out)}, nil
}

func (b *builderTools) listAuthProviders(ctx context.Context, call tools.Call) (tools.Result, error) {
	owner, appID, err := b.ownerAndApp(ctx, call.SessionID)
	if err != nil {
		return tools.Result{}, err
	}
	providers, err := GetAppAuthProviders(ctx, b.deps.Pool, appID, owner)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return tools.Result{}, tools.ToolErrorf("%s", builderAppUnavailableMsg)
		}
		return tools.Result{}, fmt.Errorf("dashboard: builder tools: list auth providers: %w", err)
	}
	return tools.Result{Content: string(providers)}, nil
}

func (b *builderTools) proposeBackendChange(ctx context.Context, call tools.Call) (tools.Result, error) {
	op, err := ParseBuilderProposal(call.Arguments)
	if err != nil {
		return tools.Result{}, err
	}
	// A proposal for an app the owner can no longer see is pointless and would
	// only fail later at confirm time; fail now with the same tool error.
	owner, appID, err := b.ownerAndApp(ctx, call.SessionID)
	if err != nil {
		return tools.Result{}, err
	}
	role, err := ResolveAppRole(ctx, b.deps.Pool, owner, AppRef{BackendAppID: appID})
	if err != nil {
		return tools.Result{}, fmt.Errorf("dashboard: builder tools: resolve role: %w", err)
	}
	if !role.Effective() {
		return tools.Result{}, tools.ToolErrorf("%s", builderAppUnavailableMsg)
	}
	return tools.Result{Content: fmt.Sprintf(
		"Proposal %q recorded and sent to the user for confirmation. It has NOT been applied. Stop here and wait: the user will confirm or reject it in the dashboard.", op.Kind)}, nil
}

const proposeBackendChangeSchema = `{
  "type": "object",
  "properties": {
    "operation": {
      "type": "object",
      "description": "Exactly one backend operation. Set \"kind\" and the payload under the key named like the kind.",
      "properties": {
        "kind": {"type": "string", "enum": ["add_table", "add_column", "add_index", "add_reference", "add_foreign_key", "remove_foreign_key", "set_rls_mode", "toggle_auth"]},
        "add_table": {"type": "object", "properties": {"name": {"type": "string"}, "columns": {"type": "array", "items": {"$ref": "#/$defs/column"}}}, "required": ["name", "columns"]},
        "add_column": {"type": "object", "properties": {"table": {"type": "string"}, "column": {"$ref": "#/$defs/column"}}, "required": ["table", "column"]},
        "add_index": {"type": "object", "properties": {"table": {"type": "string"}, "name": {"type": "string"}, "columns": {"type": "array", "items": {"type": "string"}}, "unique": {"type": "boolean"}}, "required": ["table", "name", "columns"]},
        "add_reference": {"type": "object", "properties": {"table": {"type": "string"}, "column": {"$ref": "#/$defs/column"}, "ref_table": {"type": "string"}, "ref_column": {"type": "string"}, "on_delete": {"type": "string"}}, "required": ["table", "column", "ref_table", "ref_column"]},
        "add_foreign_key": {"type": "object", "properties": {"table": {"type": "string"}, "column": {"type": "string"}, "ref_table": {"type": "string"}, "ref_column": {"type": "string"}, "on_delete": {"type": "string"}}, "required": ["table", "column", "ref_table", "ref_column"]},
        "remove_foreign_key": {"type": "object", "properties": {"table": {"type": "string"}, "column": {"type": "string"}}, "required": ["table", "column"]},
        "set_rls_mode": {"type": "object", "properties": {"table": {"type": "string"}, "mode": {"type": "string"}}, "required": ["table", "mode"]},
        "toggle_auth": {"type": "object", "properties": {"email_enabled": {"type": "boolean"}}, "required": ["email_enabled"]}
      },
      "required": ["kind"]
    }
  },
  "required": ["operation"],
  "additionalProperties": false,
  "$defs": {
    "column": {"type": "object", "properties": {"name": {"type": "string"}, "type": {"type": "string"}, "allowed_values": {"type": "array", "items": {"type": "string"}}}, "required": ["name", "type"]}
  }
}`

// builderOperationKinds are the accepted EditOperation kinds.
var builderOperationKinds = map[string]bool{
	"add_table": true, "add_column": true, "add_index": true, "add_reference": true,
	"add_foreign_key": true, "remove_foreign_key": true, "set_rls_mode": true, "toggle_auth": true,
}

// ParseBuilderProposal validates the raw arguments of a propose_backend_change
// call into an ai.EditOperation. Every failure is a *tools.ToolError the model
// can act on; nothing from the Go decoder is echoed beyond tools.DecodeArgs'
// sanitized messages. T27 (confirm) re-parses the stored call arguments with
// this function so the applied operation is exactly the validated one.
func ParseBuilderProposal(raw json.RawMessage) (*ai.EditOperation, error) {
	var args struct {
		Operation json.RawMessage `json:"operation"`
	}
	if err := tools.DecodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Operation) == 0 || string(args.Operation) == "null" {
		return nil, tools.ToolErrorf("invalid arguments: \"operation\" is required")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args.Operation, &fields); err != nil || fields == nil {
		return nil, tools.ToolErrorf("invalid arguments: \"operation\" must be an object")
	}
	var kind string
	if k, ok := fields["kind"]; !ok || json.Unmarshal(k, &kind) != nil || kind == "" {
		return nil, tools.ToolErrorf("invalid operation: \"kind\" must be a non-empty string")
	}
	if !builderOperationKinds[kind] {
		return nil, tools.ToolErrorf("invalid operation: unknown kind %q", truncateForModel(kind, 64))
	}
	for key, val := range fields {
		if key == "kind" {
			continue
		}
		if !builderOperationKinds[key] {
			return nil, tools.ToolErrorf("invalid operation: unknown field %q", truncateForModel(key, 64))
		}
		if key != kind && string(val) != "null" {
			return nil, tools.ToolErrorf("invalid operation: exactly one operation is allowed, got %q alongside kind %q", truncateForModel(key, 64), kind)
		}
	}
	payload, ok := fields[kind]
	if !ok || strings.TrimSpace(string(payload)) == "null" || len(payload) == 0 || payload[0] != '{' {
		return nil, tools.ToolErrorf("invalid operation: %q must be an object under the key %q", kind, kind)
	}
	if kind == "toggle_auth" {
		// An absent email_enabled would decode as false and silently disable
		// email login, so it must be explicit.
		var p map[string]json.RawMessage
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, tools.ToolErrorf("invalid operation: %q must be an object", kind)
		}
		var v bool
		if e, present := p["email_enabled"]; !present || json.Unmarshal(e, &v) != nil {
			return nil, tools.ToolErrorf("invalid operation: toggle_auth requires a boolean \"email_enabled\"")
		}
	}
	op, err := ai.ParseEditOperation("propose_"+kind, string(payload))
	if err != nil {
		return nil, tools.ToolErrorf("invalid operation: %q is malformed or missing required fields", kind)
	}
	return op, nil
}

func truncateForModel(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
