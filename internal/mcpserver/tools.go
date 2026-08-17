// Tool registrations for the MCP server (mcp-server spec design.md: MCP
// Tool Registry). Every tool is a thin wrapper around a shared *ForUser
// operation function already used by the equivalent REST handler — the
// mechanism that satisfies the spec's "every MCP tool call executes through
// the exact same authorization and validation path" goal. Internal errors
// are mapped to a fixed generic message before being returned (AGENTS.md
// §4: never leak err.Error() into a caller-facing surface for 500s); each
// ToolHandlerFor's returned error becomes a structured tool-error result
// (CallToolResult.IsError, per the SDK's own contract), never a raw Go
// error string.
package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/policytemplates"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// ToolDeps bundles what every registered tool needs: the shared Postgres
// pool the read-only *ForUser functions take directly (T10), plus the
// *dashboard.Handler the write *ForUser methods (T4-T7) are bound to —
// same Handler instance internal/server/server.go's REST routes already
// use, so a write tool call runs through the identical provisioner/audit
// wiring a REST call would.
type ToolDeps struct {
	Pool  *db.Pool
	DashH *dashboard.Handler
}

// errInternal is the fixed, safe-to-expose message every tool returns for
// an internal (500-class) failure — the real error is never surfaced to the
// MCP caller (AGENTS.md §4).
var errInternal = errors.New("internal error")

// errUnauthorized signals the (should-be-unreachable) case where a tool
// handler runs without a DashboardUser in context — RequirePAT guarantees
// one for every request that reaches the MCP layer.
var errUnauthorized = errors.New("unauthorized")

// RegisterTools registers every mcp-server spec tool against server. Called
// once per NewHandler construction.
func RegisterTools(server *mcp.Server, deps ToolDeps) {
	registerReadTools(server, deps)
	registerWriteTools(server, deps)
	registerTemplateTools(server, deps)
	registerAppConfigReadTools(server, deps)
	registerAccessReadTools(server, deps)
	registerOperationalReadTools(server, deps)
}

// orbitListAppsInput takes no arguments — the tool always lists the calling
// PAT's own apps, mirroring GET /dashboard/api/apps (no filter params).
type orbitListAppsInput struct{}

// orbitListAppsOutput mirrors ListApps' REST response shape (a JSON array
// of apps) wrapped in an object, since MCP tool outputs are objects.
type orbitListAppsOutput struct {
	Apps []*dashboard.AppRow `json:"apps"`
}

// orbitGetAppSchemaInput is the input for orbit_get_app_schema.
type orbitGetAppSchemaInput struct {
	AppID string `json:"app_id" jsonschema:"id of the app to fetch the schema for"`
}

// registerReadTools registers orbit_list_apps and orbit_get_app_schema —
// the two lowest-risk, read-only tools (mcp-server spec T10), proving the
// registration pattern before any write tool is added.
func registerReadTools(server *mcp.Server, deps ToolDeps) {
	// Out is `any` for both tools below rather than the concrete response
	// struct: AddTool's automatic output-schema inference can't express
	// AppRow.AuthProviders (json.RawMessage, a raw JSON object at runtime)
	// as a JSON Schema type, and the SDK docs call out `any` as the escape
	// hatch ("if the output type is 'any', no output schema is generated").
	// StructuredContent is still populated correctly from the returned
	// value — only the automatic schema *validation* is skipped.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_apps",
		Description: "List every app the caller's dashboard account has access to.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ orbitListAppsInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		apps, err := dashboard.ListAppsForUser(ctx, deps.Pool, user)
		if err != nil {
			return nil, nil, errInternal
		}
		return nil, orbitListAppsOutput{Apps: apps}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_get_app_schema",
		Description: "Get an app's current tables, columns, RLS modes, and row policies.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitGetAppSchemaInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		schema, err := dashboard.GetAppSchemaForUser(ctx, deps.Pool, user, in.AppID)
		if err != nil {
			if errors.Is(err, dashboard.ErrNotFound) {
				return nil, nil, errors.New("not found")
			}
			return nil, nil, errInternal
		}
		return nil, schema, nil
	})
}

// orbitCreateAppInput mirrors the minimal slice of dashboard's
// appRequestBody a "describe an app, get an app" tool call needs (mcp-server
// spec P2 AC1: "valid name and configuration") — name plus whether the
// app's own end users authenticate with email/password. The richer
// sub-objects (auth_providers, storage_config, rate_limit) stay REST/UI-only
// for now; nothing in the spec's P2 story requires an LLM to set them.
type orbitCreateAppInput struct {
	Name             string `json:"name" jsonschema:"the app's name (lowercase letters, numbers, and hyphens)"`
	AuthEmailEnabled bool   `json:"auth_email_enabled,omitempty" jsonschema:"whether this app's end users authenticate with email/password"`
}

// orbitCreateTableInput mirrors dashboard's tableRequestBody plus the
// target app id (tableRequestBody's id normally comes from the URL path).
type orbitCreateTableInput struct {
	AppID   string                `json:"app_id" jsonschema:"id of the app to add the table to"`
	Name    string                `json:"name" jsonschema:"the table's name"`
	RLS     string                `json:"rls,omitempty" jsonschema:"row-level security mode: one of \"\", \"owner\", \"enabled\", \"policy\""`
	Columns []config.ColumnConfig `json:"columns" jsonschema:"the table's columns"`
	Indexes []config.IndexConfig  `json:"indexes,omitempty" jsonschema:"indexes to create on the table"`
}

// orbitSetTableRLSModeInput is the input for orbit_set_table_rls_mode.
type orbitSetTableRLSModeInput struct {
	AppID     string `json:"app_id" jsonschema:"id of the app that owns the table"`
	TableName string `json:"table_name" jsonschema:"name of the table to update"`
	RLSMode   string `json:"rls_mode" jsonschema:"one of \"\", \"owner\", \"enabled\", \"policy\""`
}

// mapWriteError maps an error returned by one of the write *ForUser
// functions to a caller-safe tool error: *dashboard.ValidationError and
// *provisioner.TypeChangeError messages are safe to expose verbatim (same
// "safe to expose" exception AGENTS.md §4 carves out for
// provisioner.ValidationError — the caller-input-naming message is the
// point of MCP-08), ErrNotFound/ErrForbidden map to their REST-equivalent
// wording, and everything else collapses to the fixed generic message.
func mapWriteError(err error) error {
	var valErr *dashboard.ValidationError
	var typeErr *provisioner.TypeChangeError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, dashboard.ErrNotFound):
		return errors.New("not found")
	case errors.Is(err, dashboard.ErrForbidden):
		return errors.New("forbidden")
	case errors.Is(err, dashboard.ErrTableNotFound):
		return errors.New("table not found")
	case errors.Is(err, dashboard.ErrPolicyAlreadyExists):
		return dashboard.ErrPolicyAlreadyExists
	case errors.As(err, &valErr):
		return valErr
	case errors.As(err, &typeErr):
		return typeErr
	default:
		return errInternal
	}
}

// registerWriteTools registers the three core write tools (mcp-server spec
// T11) — each a thin wrapper over a Phase 2 *ForUser method, so a tool call
// runs through the exact same validation/provisioner/audit path the
// equivalent REST endpoint uses.
func registerWriteTools(server *mcp.Server, deps ToolDeps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_create_app",
		Description: "Create a new app.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitCreateAppInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		app, err := deps.DashH.CreateAppForUser(ctx, user, dashboard.AppRequestBody{
			Name:             in.Name,
			AuthEmailEnabled: in.AuthEmailEnabled,
		}, "mcp")
		if err != nil {
			return nil, nil, mapWriteError(err)
		}
		return nil, app, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_create_table",
		Description: "Create a table (with columns) on an existing app.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitCreateTableInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		row, err := deps.DashH.CreateAppTableForUser(ctx, user, in.AppID, dashboard.TableRequestBody{
			Name:    in.Name,
			RLS:     in.RLS,
			Columns: in.Columns,
			Indexes: in.Indexes,
		}, "mcp")
		if err != nil {
			return nil, nil, mapWriteError(err)
		}
		return nil, row, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_set_table_rls_mode",
		Description: "Set a table's row-level security mode.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitSetTableRLSModeInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		row, err := deps.DashH.UpdateTableRLSModeForUser(ctx, user, in.AppID, in.TableName, in.RLSMode, "mcp")
		if err != nil {
			return nil, nil, mapWriteError(err)
		}
		return nil, row, nil
	})
}

// orbitListPolicyTemplatesInput takes no arguments — the template catalog
// is fixed and global, not per-app.
type orbitListPolicyTemplatesInput struct{}

// orbitPolicyTemplateInfo describes one template for orbit_list_policy_templates
// — policytemplates.TemplateDefinition alone (id/requires-owner-column/kind/
// fixed-actions) isn't enough for an LLM to pick a template without guessing
// free-form clause syntax (mcp-server spec T12/MCP-12), so this adds a
// human-readable description and the exact input names
// orbit_create_policy_from_template requires for that template.
type orbitPolicyTemplateInfo struct {
	ID                  string   `json:"id"`
	Description         string   `json:"description"`
	RequiresOwnerColumn bool     `json:"requires_owner_column"`
	Kind                string   `json:"kind"`
	ActionsFixed        []string `json:"actions_fixed,omitempty"`
	RequiredInputs      []string `json:"required_inputs"`
}

// policyTemplateDescriptions/policyTemplateRequiredInputs are MCP-tool-level
// metadata only — policytemplates itself stays pure per its own package doc
// ("Dependencies: none"); this mapping is not persisted or reused elsewhere.
var policyTemplateDescriptions = map[string]string{
	policytemplates.TemplateIDOwnerOnly:          "Only the row's own owner can access it, for the chosen actions.",
	policytemplates.TemplateIDOpenRead:           "Any of the given roles can read every row; no write access granted.",
	policytemplates.TemplateIDReadOnly:           "Alias of open_read — nobody can write, matching roles can read every row.",
	policytemplates.TemplateIDValueMatch:         "Read access restricted to rows where a chosen column equals a fixed value.",
	policytemplates.TemplateIDOpenReadOwnerWrite: "Any of the given roles can read every row; only the row's owner can update or delete it.",
	policytemplates.TemplateIDBlockedByDefault:   "Informational only — creates no policy. Set the table's rls mode to deny access by default instead.",
}

var policyTemplateRequiredInputs = map[string][]string{
	policytemplates.TemplateIDOwnerOnly:          {"actions", "roles"},
	policytemplates.TemplateIDOpenRead:           {"roles"},
	policytemplates.TemplateIDReadOnly:           {"roles"},
	policytemplates.TemplateIDValueMatch:         {"roles", "column", "value"},
	policytemplates.TemplateIDOpenReadOwnerWrite: {"roles"},
	policytemplates.TemplateIDBlockedByDefault:   {},
}

// orbitCreatePolicyFromTemplateInput is the input for
// orbit_create_policy_from_template. Actions/Column/Value are only required
// for specific templates (owner_only, value_match respectively) — see
// policyTemplateRequiredInputs.
type orbitCreatePolicyFromTemplateInput struct {
	AppID      string   `json:"app_id" jsonschema:"id of the app that owns the table"`
	TableName  string   `json:"table_name" jsonschema:"name of the table to create the policy/policies on"`
	TemplateID string   `json:"template_id" jsonschema:"one of the ids returned by orbit_list_policy_templates"`
	Actions    []string `json:"actions,omitempty" jsonschema:"required for owner_only: the actions to restrict to the row's owner (e.g. select, update)"`
	Roles      []string `json:"roles,omitempty" jsonschema:"the end-user roles this policy applies to"`
	Column     string   `json:"column,omitempty" jsonschema:"required for value_match: the column to filter on"`
	Value      string   `json:"value,omitempty" jsonschema:"required for value_match: the literal value the column must equal"`
}

// orbitCreatePolicyFromTemplateResult reports which policies were created
// and, on a partial failure, which policy failed and which were never
// attempted — the same partial-failure contract the frontend template
// picker already has (mcp-server spec MCP-13, design.md Error Handling
// Strategy: "stop at first error, report which policies succeeded and
// which step failed").
type orbitCreatePolicyFromTemplateResult struct {
	Created       []*dashboard.TablePolicyRow `json:"created"`
	FailedPolicy  string                      `json:"failed_policy,omitempty"`
	FailureReason string                      `json:"failure_reason,omitempty"`
	Pending       []string                    `json:"pending_policies,omitempty"`
}

// missingInputError builds the structured error orbit_create_policy_from_template
// returns for a missing/invalid required input (mcp-server spec MCP-14):
// names the specific input, so the LLM knows exactly what to re-ask for.
func missingInputError(name string) error {
	return errors.New("missing or invalid input: " + name)
}

// toDashboardPolicyDef converts a policytemplates.PolicyDef (the pure,
// dependency-free package's own type) into dashboard.PolicyDef — the two
// are a deliberate field-for-field mirror (see policytemplates' package
// doc), so this is a plain field copy, not a transformation.
func toDashboardPolicyDef(p policytemplates.PolicyDef) dashboard.PolicyDef {
	clauses := make([]dashboard.PolicyClause, 0, len(p.Clauses))
	for _, c := range p.Clauses {
		clauses = append(clauses, dashboard.PolicyClause{
			Column:      c.Column,
			Operator:    c.Operator,
			ValueSource: c.ValueSource,
			Value:       c.Value,
			Logic:       c.Logic,
		})
	}
	return dashboard.PolicyDef{
		Name:    p.Name,
		Action:  p.Action,
		Roles:   p.Roles,
		Clauses: clauses,
	}
}

// buildTemplatePolicies resolves in.TemplateID + its inputs into the
// PolicyDef(s) that template produces, or a missingInputError if a
// template-specific required input is missing/empty (mcp-server spec
// MCP-14) — evaluated entirely before any policy is created.
func buildTemplatePolicies(in orbitCreatePolicyFromTemplateInput) ([]policytemplates.PolicyDef, error) {
	switch in.TemplateID {
	case policytemplates.TemplateIDOwnerOnly:
		if len(in.Roles) == 0 {
			return nil, missingInputError("roles")
		}
		if len(in.Actions) == 0 {
			return nil, missingInputError("actions")
		}
		return policytemplates.BuildOwnerOnlyPolicies(in.Actions, in.Roles), nil
	case policytemplates.TemplateIDOpenRead:
		if len(in.Roles) == 0 {
			return nil, missingInputError("roles")
		}
		return []policytemplates.PolicyDef{policytemplates.BuildOpenReadPolicy(in.Roles)}, nil
	case policytemplates.TemplateIDReadOnly:
		if len(in.Roles) == 0 {
			return nil, missingInputError("roles")
		}
		return []policytemplates.PolicyDef{policytemplates.BuildReadOnlyPolicy(in.Roles)}, nil
	case policytemplates.TemplateIDValueMatch:
		if len(in.Roles) == 0 {
			return nil, missingInputError("roles")
		}
		if in.Column == "" {
			return nil, missingInputError("column")
		}
		if in.Value == "" {
			return nil, missingInputError("value")
		}
		return []policytemplates.PolicyDef{policytemplates.BuildValueMatchPolicy(in.Column, in.Value, in.Roles)}, nil
	case policytemplates.TemplateIDOpenReadOwnerWrite:
		if len(in.Roles) == 0 {
			return nil, missingInputError("roles")
		}
		return policytemplates.BuildOpenReadOwnerWritePolicies(in.Roles), nil
	case policytemplates.TemplateIDBlockedByDefault:
		return nil, errors.New("template blocked_by_default creates no policy — set the table's rls mode instead")
	default:
		return nil, missingInputError("template_id")
	}
}

// registerTemplateTools registers orbit_list_policy_templates and
// orbit_create_policy_from_template (mcp-server spec T12).
func registerTemplateTools(server *mcp.Server, deps ToolDeps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_policy_templates",
		Description: "List the named row-policy templates available for orbit_create_policy_from_template.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ orbitListPolicyTemplatesInput) (*mcp.CallToolResult, any, error) {
		defs := policytemplates.List()
		out := make([]orbitPolicyTemplateInfo, 0, len(defs))
		for _, d := range defs {
			out = append(out, orbitPolicyTemplateInfo{
				ID:                  d.ID,
				Description:         policyTemplateDescriptions[d.ID],
				RequiresOwnerColumn: d.RequiresOwnerColumn,
				Kind:                d.Kind,
				ActionsFixed:        d.ActionsFixed,
				RequiredInputs:      policyTemplateRequiredInputs[d.ID],
			})
		}
		return nil, struct {
			Templates []orbitPolicyTemplateInfo `json:"templates"`
		}{Templates: out}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_create_policy_from_template",
		Description: "Create a row policy (or set of policies) on a table from a named template.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitCreatePolicyFromTemplateInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}

		defs, err := buildTemplatePolicies(in)
		if err != nil {
			return nil, nil, err
		}

		created := make([]*dashboard.TablePolicyRow, 0, len(defs))
		for i, def := range defs {
			row, err := deps.DashH.CreateTablePolicyForUser(ctx, user, in.AppID, in.TableName, toDashboardPolicyDef(def), "mcp")
			if err != nil {
				pending := make([]string, 0, len(defs)-i-1)
				for _, remaining := range defs[i+1:] {
					pending = append(pending, remaining.Name)
				}
				return nil, orbitCreatePolicyFromTemplateResult{
					Created:       created,
					FailedPolicy:  def.Name,
					FailureReason: mapWriteError(err).Error(),
					Pending:       pending,
				}, nil
			}
			created = append(created, row)
		}
		return nil, orbitCreatePolicyFromTemplateResult{Created: created}, nil
	})
}

// mapReadError maps an error returned by one of the new read-only *ForUser
// functions (mcp-read-only-tools spec) to a caller-safe tool error, the
// same fixed-message convention mapWriteError already established:
// ErrNotFound/ErrForbidden map to their REST-equivalent wording, everything
// else collapses to errInternal (AGENTS.md §4 — never a raw err.Error()).
func mapReadError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, dashboard.ErrNotFound):
		return errors.New("not found")
	case errors.Is(err, dashboard.ErrForbidden):
		return errors.New("forbidden")
	case errors.Is(err, dashboard.ErrTableNotFound):
		return errors.New("table not found")
	case errors.Is(err, dashboard.ErrAppTokensNotSupported):
		return dashboard.ErrAppTokensNotSupported
	case errors.Is(err, dashboard.ErrWebhookNotFound):
		return errors.New("webhook not found")
	default:
		return errInternal
	}
}

// orbitGetAppInput is the input for orbit_get_app.
type orbitGetAppInput struct {
	AppID string `json:"app_id" jsonschema:"id of the app to fetch"`
}

// orbitListAppAuthProvidersInput is the input for orbit_list_app_auth_providers.
type orbitListAppAuthProvidersInput struct {
	AppID string `json:"app_id" jsonschema:"id of the app to fetch auth providers for"`
}

// orbitListTablePoliciesInput is the input for orbit_list_table_policies.
type orbitListTablePoliciesInput struct {
	AppID     string `json:"app_id" jsonschema:"id of the app that owns the table"`
	TableName string `json:"table_name" jsonschema:"name of the table to list row policies for"`
}

// orbitListTablePoliciesOutput mirrors ListTablePolicies' REST response
// shape (a JSON array of policies) wrapped in an object, since MCP tool
// outputs are objects.
type orbitListTablePoliciesOutput struct {
	Policies []dashboard.TablePolicyRow `json:"policies"`
}

// registerAppConfigReadTools registers the read-only tools that expose an
// app's own configuration record and per-table policy data (mcp-read-only-tools
// spec P1: orbit_get_app; T2 adds orbit_list_app_auth_providers; T5 adds
// orbit_list_table_policies here too). Each tool authorizes through the
// exact tier its wrapped function/REST handler already enforces — the
// tiers are not uniform across this group, see design.md's Authorization
// Matrix.
func registerAppConfigReadTools(server *mcp.Server, deps ToolDeps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_get_app",
		Description: "Get an app's own configuration record (auth providers, storage, rate limit — secrets redacted). Requires the caller to have any effective role on the app (same visibility GetApp already grants), no extra management role required.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitGetAppInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		app, _, err := dashboard.GetApp(ctx, deps.Pool, in.AppID, user)
		if err != nil {
			return nil, nil, mapReadError(err)
		}
		app.RedactSecrets()
		return nil, app, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_app_auth_providers",
		Description: "List an app's configured login providers (client secrets redacted to a client_secret_set boolean). Requires the caller to have any effective role on the app, no extra management role required.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitListAppAuthProvidersInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		providers, err := dashboard.GetAppAuthProviders(ctx, deps.Pool, in.AppID, user)
		if err != nil {
			return nil, nil, mapReadError(err)
		}
		return nil, providers, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_table_policies",
		Description: "List every row policy on a table. Requires the caller to be able to manage the app (role.CanManage()) — table policies are part of the app's access-control surface, same tier as CreateTablePolicy.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitListTablePoliciesInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		policies, err := dashboard.ListTablePoliciesForUser(ctx, deps.Pool, user, in.AppID, in.TableName)
		if err != nil {
			return nil, nil, mapReadError(err)
		}
		return nil, orbitListTablePoliciesOutput{Policies: policies}, nil
	})
}

// orbitListMyPatsInput takes no arguments — the tool always lists the
// calling identity's own PATs, mirroring GET /dashboard/api/me/pats
// (mcp-read-only-tools spec: "ListPATs scope" assumption — not app-scoped,
// PATs aren't tied to one app).
type orbitListMyPatsInput struct{}

// orbitListMyPatsOutput mirrors ListPATs' REST response shape wrapped in an
// object, since MCP tool outputs are objects.
type orbitListMyPatsOutput struct {
	PATs []dashboard.PATRow `json:"pats"`
}

// orbitListAppMembersInput is the input for orbit_list_app_members.
type orbitListAppMembersInput struct {
	AppID string `json:"app_id" jsonschema:"id of the app to list members for"`
}

// orbitListAppMembersOutput mirrors ListAppMembers' REST response shape
// (a JSON array of members) wrapped in an object, since MCP tool outputs
// are objects.
type orbitListAppMembersOutput struct {
	Members []*dashboard.AppMember `json:"members"`
}

// orbitListAppTokensInput is the input for orbit_list_app_tokens.
type orbitListAppTokensInput struct {
	AppID string `json:"app_id" jsonschema:"id of the app to list issued API tokens for"`
}

// orbitListAppTokensOutput mirrors ListAppTokens' REST response shape (a
// JSON array of token metadata rows) wrapped in an object, since MCP tool
// outputs are objects.
type orbitListAppTokensOutput struct {
	Tokens []dashboard.AppTokenRow `json:"tokens"`
}

// registerAccessReadTools registers the read-only tools that expose who
// and what has access to an app, plus the caller's own identity-scoped
// resources (mcp-read-only-tools spec P2: T3 adds orbit_list_my_pats; T7
// adds orbit_list_app_members; T9 adds orbit_list_app_tokens here too).
func registerAccessReadTools(server *mcp.Server, deps ToolDeps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_my_pats",
		Description: "List the calling identity's own personal access tokens (metadata only — no raw token value). Not app-scoped: returns the caller's PATs regardless of any app.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ orbitListMyPatsInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		pats, err := dashboard.ListPATs(ctx, deps.Pool, user.ID)
		if err != nil {
			return nil, nil, errInternal
		}
		return nil, orbitListMyPatsOutput{PATs: pats}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_app_members",
		Description: "List an app's team members (user_id, role, created_at). Requires the caller to be able to manage the app (role.CanManage()) — membership is part of the app's access-control surface, same tier as table policies.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitListAppMembersInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		members, err := dashboard.ListAppMembersForUser(ctx, deps.Pool, user, in.AppID)
		if err != nil {
			return nil, nil, mapReadError(err)
		}
		return nil, orbitListAppMembersOutput{Members: members}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_app_tokens",
		Description: "List an app's issued API tokens (metadata only — no raw token/JTI value). Requires the caller to have any effective role on the app (same visibility GetApp already grants), no extra management role required. Not available for apps with email/password auth enabled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitListAppTokensInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		tokens, err := dashboard.ListAppTokensForUser(ctx, deps.Pool, user, in.AppID)
		if err != nil {
			return nil, nil, mapReadError(err)
		}
		return nil, orbitListAppTokensOutput{Tokens: tokens}, nil
	})
}

// orbitWebhookSummary is the MCP-facing shape of a dashboard.WebhookRow —
// the same fields webhooks_handler.go's webhookResponse exposes over REST,
// minus Token: webhookResponse decrypts and returns the plaintext callback
// token for the dashboard session's own use, but design.md's Tech Decisions
// require "no signing-secret value" for the read-only MCP tools, so the
// token is never included here (WebhookRow's own TokenSecret ciphertext
// field is dropped too — same reasoning, different form of the same
// secret). WebhookRow itself carries no json tags, so this translation also
// avoids emitting Go's default PascalCase field names.
type orbitWebhookSummary struct {
	ID             string         `json:"id"`
	AppID          string         `json:"app_id"`
	Name           string         `json:"name"`
	Method         string         `json:"method"`
	EventTypePath  string         `json:"event_type_path"`
	EventIDPath    *string        `json:"event_id_path"`
	Status         string         `json:"status"`
	CapturedSample map[string]any `json:"captured_sample"`
	DeletedAt      *time.Time     `json:"deleted_at"`
	CreatedBy      string         `json:"created_by"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func toOrbitWebhookSummary(w dashboard.WebhookRow) orbitWebhookSummary {
	return orbitWebhookSummary{
		ID:             w.ID,
		AppID:          w.AppID,
		Name:           w.Name,
		Method:         w.Method,
		EventTypePath:  w.EventTypePath,
		EventIDPath:    w.EventIDPath,
		Status:         w.Status,
		CapturedSample: w.CapturedSample,
		DeletedAt:      w.DeletedAt,
		CreatedBy:      w.CreatedBy,
		CreatedAt:      w.CreatedAt,
		UpdatedAt:      w.UpdatedAt,
	}
}

// orbitEventMapping is the MCP-facing shape of a dashboard.EventMappingRow
// (also carries no json tags in its store form) — snake_case, matching
// webhooks_handler.go's own mappingResponse translation.
type orbitEventMapping struct {
	ID             string                      `json:"id"`
	WebhookID      string                      `json:"webhook_id"`
	EventTypeValue string                      `json:"event_type_value"`
	Action         string                      `json:"action"`
	TargetTable    string                      `json:"target_table"`
	MatchKeyColumn *string                     `json:"match_key_column"`
	FieldMappings  []dashboard.FieldMappingDef `json:"field_mappings"`
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
}

func toOrbitEventMapping(m dashboard.EventMappingRow) orbitEventMapping {
	return orbitEventMapping{
		ID:             m.ID,
		WebhookID:      m.WebhookID,
		EventTypeValue: m.EventTypeValue,
		Action:         m.Action,
		TargetTable:    m.TargetTable,
		MatchKeyColumn: m.MatchKeyColumn,
		FieldMappings:  m.FieldMappings,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

// orbitListWebhooksInput is the input for orbit_list_webhooks.
type orbitListWebhooksInput struct {
	AppID string `json:"app_id" jsonschema:"id of the app to list webhooks for"`
}

// orbitListWebhooksOutput wraps a JSON array of webhook summaries, since MCP
// tool outputs are objects.
type orbitListWebhooksOutput struct {
	Webhooks []orbitWebhookSummary `json:"webhooks"`
}

// orbitGetWebhookInput is the input for orbit_get_webhook.
type orbitGetWebhookInput struct {
	AppID     string `json:"app_id" jsonschema:"id of the app that owns the webhook"`
	WebhookID string `json:"webhook_id" jsonschema:"id of the webhook to fetch"`
}

// orbitGetWebhookOutput combines a webhook's config with its event mappings
// in one response (design.md Tech Decisions: matches spec AC2 exactly,
// avoids a second round-trip for what's conceptually one "show me this
// webhook" question).
type orbitGetWebhookOutput struct {
	Webhook       orbitWebhookSummary `json:"webhook"`
	EventMappings []orbitEventMapping `json:"event_mappings"`
}

// orbitListWebhookDeliveriesInput is the input for
// orbit_list_webhook_deliveries. Limit/offset mirror the same bounds the
// REST endpoint already enforces (ListWebhookDeliveriesForUser clamps
// silently, it never rejects) — no new query capability is invented here,
// per design.md's Tech Decisions.
type orbitListWebhookDeliveriesInput struct {
	AppID     string `json:"app_id" jsonschema:"id of the app that owns the webhook"`
	WebhookID string `json:"webhook_id" jsonschema:"id of the webhook to list deliveries for"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max rows to return (default 50, max 200)"`
	Offset    int    `json:"offset,omitempty" jsonschema:"rows to skip (default 0)"`
}

// orbitDelivery is the MCP-facing shape of a dashboard.DeliveryRow (also
// carries no json tags in its store form) — snake_case, matching
// webhooks_handler.go's own deliveryResponse translation.
type orbitDelivery struct {
	ID             string         `json:"id"`
	WebhookID      string         `json:"webhook_id"`
	ReceivedAt     time.Time      `json:"received_at"`
	HTTPStatus     int            `json:"http_status"`
	Outcome        string         `json:"outcome"`
	EventTypeValue *string        `json:"event_type_value"`
	EventID        *string        `json:"event_id"`
	RawPayload     map[string]any `json:"raw_payload"`
	TargetRowID    *string        `json:"target_row_id"`
	ErrorDetail    *string        `json:"error_detail"`
}

func toOrbitDelivery(d dashboard.DeliveryRow) orbitDelivery {
	return orbitDelivery{
		ID:             d.ID,
		WebhookID:      d.WebhookID,
		ReceivedAt:     d.ReceivedAt,
		HTTPStatus:     d.HTTPStatus,
		Outcome:        d.Outcome,
		EventTypeValue: d.EventTypeValue,
		EventID:        d.EventID,
		RawPayload:     d.RawPayload,
		TargetRowID:    d.TargetRowID,
		ErrorDetail:    d.ErrorDetail,
	}
}

// orbitListWebhookDeliveriesOutput mirrors ListWebhookDeliveries' REST
// response shape (a JSON array of deliveries) wrapped in an object.
type orbitListWebhookDeliveriesOutput struct {
	Deliveries []orbitDelivery `json:"deliveries"`
}

// registerOperationalReadTools registers the read-only tools that expose an
// app's operational history — webhooks, their event mappings, delivery
// history, and (T15) caller-wide log metrics (mcp-read-only-tools spec P3:
// T13 adds the three webhook tools here). All three webhook tools share the
// same CanManage() tier as table policies/members (design.md's
// Authorization Matrix).
func registerOperationalReadTools(server *mcp.Server, deps ToolDeps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_webhooks",
		Description: "List every webhook configured for an app (no signing-secret value). Requires the caller to be able to manage the app (role.CanManage()).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitListWebhooksInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		rows, err := dashboard.ListWebhooksForUser(ctx, deps.Pool, user, in.AppID)
		if err != nil {
			return nil, nil, mapReadError(err)
		}
		out := make([]orbitWebhookSummary, 0, len(rows))
		for _, row := range rows {
			out = append(out, toOrbitWebhookSummary(row))
		}
		return nil, orbitListWebhooksOutput{Webhooks: out}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_get_webhook",
		Description: "Get one webhook's full config (no signing-secret value) plus its event mappings. Requires the caller to be able to manage the app (role.CanManage()). webhook_id must belong to app_id — a webhook from a different app returns not-found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitGetWebhookInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		wh, mappings, err := dashboard.GetWebhookForUser(ctx, deps.Pool, user, in.AppID, in.WebhookID)
		if err != nil {
			return nil, nil, mapReadError(err)
		}
		outMappings := make([]orbitEventMapping, 0, len(mappings))
		for _, m := range mappings {
			outMappings = append(outMappings, toOrbitEventMapping(m))
		}
		return nil, orbitGetWebhookOutput{Webhook: toOrbitWebhookSummary(*wh), EventMappings: outMappings}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_list_webhook_deliveries",
		Description: "List a webhook's delivery history, newest first (limit defaults to 50, max 200; offset defaults to 0 — same bounds the REST endpoint enforces). Requires the caller to be able to manage the app (role.CanManage()). webhook_id must belong to app_id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitListWebhookDeliveriesInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		rows, err := dashboard.ListWebhookDeliveriesForUser(ctx, deps.Pool, user, in.AppID, in.WebhookID, in.Limit, in.Offset)
		if err != nil {
			return nil, nil, mapReadError(err)
		}
		out := make([]orbitDelivery, 0, len(rows))
		for _, row := range rows {
			out = append(out, toOrbitDelivery(row))
		}
		return nil, orbitListWebhookDeliveriesOutput{Deliveries: out}, nil
	})
}
