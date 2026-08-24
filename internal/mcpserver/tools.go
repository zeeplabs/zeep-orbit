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
	"go.uber.org/zap"

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

// toolLogger receives the real error behind every errInternal a tool
// returns, set once by RegisterTools — mirrors what dashboard.Handler's own
// writeError does for REST 500s (AGENTS.md §4 requires both halves: log the
// real error server-side, return a fixed generic message to the caller).
// Defaults to a no-op so a call before RegisterTools runs (shouldn't happen
// in practice) never panics.
var toolLogger = zap.NewNop()

// internalErr logs err server-side and returns the fixed, safe-to-expose
// errInternal every tool's internal-failure branch returns to the caller.
func internalErr(err error) error {
	toolLogger.Error("mcp tool internal error", zap.Error(err))
	return errInternal
}

// RegisterTools registers every mcp-server spec tool against server. Called
// once per NewHandler construction.
func RegisterTools(server *mcp.Server, deps ToolDeps) {
	if l := deps.DashH.Logger(); l != nil {
		toolLogger = l
	}
	registerReadTools(server, deps)
	registerWriteTools(server, deps)
	registerTemplateTools(server, deps)
	registerAppConfigReadTools(server, deps)
	registerAccessReadTools(server, deps)
	registerOperationalReadTools(server, deps)
	registerAppConfigWriteTools(server, deps)
	registerOperationalWriteTools(server, deps)
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
			return nil, nil, internalErr(err)
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
			return nil, nil, internalErr(err)
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
	var fkErr *provisioner.ForeignKeyViolationError
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
	case errors.Is(err, dashboard.ErrColumnAlreadyExists):
		return dashboard.ErrColumnAlreadyExists
	case errors.Is(err, dashboard.ErrIndexAlreadyExists):
		return dashboard.ErrIndexAlreadyExists
	case errors.Is(err, dashboard.ErrWebhookNotFound):
		return errors.New("webhook not found")
	case errors.Is(err, dashboard.ErrUnknownTargetTable):
		return dashboard.ErrUnknownTargetTable
	case errors.Is(err, dashboard.ErrUnknownTargetColumn):
		return dashboard.ErrUnknownTargetColumn
	case errors.Is(err, dashboard.ErrMappingConflict):
		return dashboard.ErrMappingConflict
	case errors.Is(err, dashboard.ErrInvalidAction):
		return dashboard.ErrInvalidAction
	case errors.Is(err, dashboard.ErrMatchKeyRequired):
		return dashboard.ErrMatchKeyRequired
	case errors.Is(err, dashboard.ErrEventTypeValueRequired):
		return dashboard.ErrEventTypeValueRequired
	case errors.Is(err, dashboard.ErrFieldMappingsRequired):
		return dashboard.ErrFieldMappingsRequired
	case errors.Is(err, dashboard.ErrColumnAlreadyHasReference):
		return dashboard.ErrColumnAlreadyHasReference
	case errors.As(err, &valErr):
		return valErr
	case errors.As(err, &typeErr):
		return typeErr
	case errors.As(err, &fkErr):
		return fkErr
	default:
		return internalErr(err)
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

// orbitAddTableColumnInput is the input for orbit_add_table_column.
// config.ColumnConfig is used directly (same precedent as
// orbitCreateTableInput.Columns) rather than a bespoke translation struct —
// it already carries the json tags an MCP tool input needs.
type orbitAddTableColumnInput struct {
	AppID     string              `json:"app_id" jsonschema:"id of the app that owns the table"`
	TableName string              `json:"table_name" jsonschema:"name of the table to add the column to"`
	Column    config.ColumnConfig `json:"column" jsonschema:"the single new column to add"`
}

// addTableIndexBlockingWriteDisclosure is the spec P2 AC6-required warning
// that index creation briefly blocks writes to the target table (design.md's
// Tech Decisions: CREATE INDEX, not CONCURRENTLY). Extracted to a constant,
// referenced by both the tool description above and its test
// (TestOrbitAddTableIndex_DescriptionDisclosesBlockingBehavior), so a future
// copy edit can't silently drop the disclosure without also touching the
// test that guards it.
const addTableIndexBlockingWriteDisclosure = "Index creation briefly blocks writes to the target table (CREATE INDEX, not CONCURRENTLY) — avoid running against a table already receiving production traffic without a maintenance window."

// orbitAddTableIndexInput is the input for orbit_add_table_index.
type orbitAddTableIndexInput struct {
	AppID     string             `json:"app_id" jsonschema:"id of the app that owns the table"`
	TableName string             `json:"table_name" jsonschema:"name of the table to add the index to"`
	Index     config.IndexConfig `json:"index" jsonschema:"the single new index to add"`
}

// orbitUpdateAppInput is the input for orbit_update_app.
type orbitUpdateAppInput struct {
	AppID            string `json:"app_id" jsonschema:"id of the app to update"`
	AuthEmailEnabled bool   `json:"auth_email_enabled" jsonschema:"whether email/password authentication is enabled for this app"`
}

// orbitAddColumnForeignKeyInput is the input for orbit_add_column_foreign_key.
// config.ReferenceConfig is used directly (same precedent as
// orbitAddTableColumnInput.Column) rather than a bespoke translation struct.
type orbitAddColumnForeignKeyInput struct {
	AppID      string                 `json:"app_id" jsonschema:"id of the app that owns the table"`
	TableName  string                 `json:"table_name" jsonschema:"name of the table that owns the column"`
	ColumnName string                 `json:"column_name" jsonschema:"name of the already-existing column to add a foreign key to"`
	References config.ReferenceConfig `json:"references" jsonschema:"the foreign key target: table, column, and optional on_delete"`
}

// registerAppConfigWriteTools registers the additive table-schema mutation
// tools (mcp-safe-mutation-tools spec: add one column, add one index, each
// server-side-merged against the table's current stored definition so the
// request body can never omit or corrupt an existing column/index — see
// design.md's Architecture Overview, this is exactly why UpdateAppTable's
// full-replace endpoint isn't safe to expose directly) plus orbit_update_app
// (ai-edit-chat spec, AIEC-13), which closes the REST/MCP parity gap
// UpdateAppForUser (T3) would otherwise introduce. All three gate on
// role.CanWrite(), matching CreateAppTableForUser/UpdateTableRLSModeForUser.
func registerAppConfigWriteTools(server *mcp.Server, deps ToolDeps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_add_table_column",
		Description: "Add a single new column to an existing table, without resending or risking any other column already on that table.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitAddTableColumnInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		row, err := deps.DashH.AddTableColumnForUser(ctx, user, in.AppID, in.TableName, in.Column, "mcp")
		if err != nil {
			return nil, nil, mapWriteError(err)
		}
		return nil, row, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_add_table_index",
		Description: "Add a single new index to an existing table, without resending or risking the table's existing indexes. " + addTableIndexBlockingWriteDisclosure,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitAddTableIndexInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		row, err := deps.DashH.AddTableIndexForUser(ctx, user, in.AppID, in.TableName, in.Index, "mcp")
		if err != nil {
			return nil, nil, mapWriteError(err)
		}
		return nil, row, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_update_app",
		Description: "Update an existing app's email/password authentication setting.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitUpdateAppInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		app, err := deps.DashH.UpdateAppForUser(ctx, user, in.AppID, in.AuthEmailEnabled, "mcp")
		if err != nil {
			return nil, nil, mapWriteError(err)
		}
		return nil, app, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_add_column_foreign_key",
		Description: "Add a foreign key to an already-existing column, without dropping or recreating the column. Fails if the column already has a foreign key (remove it first), if the target table/column is invalid, if the physical Postgres types don't match, or if existing rows would violate the new constraint.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitAddColumnForeignKeyInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		row, err := deps.DashH.AddColumnForeignKeyForUser(ctx, user, in.AppID, in.TableName, in.ColumnName, in.References, "mcp")
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
		return internalErr(err)
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
			return nil, nil, internalErr(err)
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
		Description: "List an app's issued API tokens (metadata only — no raw/usable token value; the jti field is an opaque identifier used only for revocation lookup, not a credential). Requires the caller to have any effective role on the app (same visibility GetApp already grants), no extra management role required. Not available for apps with email/password auth enabled.",
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

// orbitGetLogsMetricsInput takes no arguments — the wrapped REST endpoint
// (LogsMetrics) itself takes no app_id or filter parameters (design.md's
// "orbit_get_logs_metrics takes no input" Tech Decision): it always
// returns one caller-wide aggregate across whichever apps ListOwnedAppNames
// grants the caller.
type orbitGetLogsMetricsInput struct{}

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

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_get_logs_metrics",
		Description: "Get a one-minute request-volume/latency/error-rate aggregate across every app the caller can see (RequestsPerApp breaks it down per app). Not app-scoped — takes no input. Ownership-only: unrestricted for superadmin/CanReadAnyApp, restricted to the caller's own apps otherwise. Reflects the last minute on whichever server instance handled this request — a load-balanced deployment may show different results per call.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ orbitGetLogsMetricsInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		metrics, err := dashboard.LogsMetricsForUser(ctx, deps.Pool, deps.DashH.Logs, user)
		if err != nil {
			return nil, nil, internalErr(err)
		}
		return nil, metrics, nil
	})
}

// orbitCreateWebhookInput is the input for orbit_create_webhook.
// dashboard.CreateWebhookInput carries no json tags (it's an internal store
// parameter struct, never serialized directly), so a bespoke translation
// struct is needed here — same reasoning as orbitWebhookSummary's existence
// for the read-side tools.
type orbitCreateWebhookInput struct {
	AppID         string `json:"app_id" jsonschema:"id of the app to create the webhook on"`
	Name          string `json:"name" jsonschema:"a human-readable name for the webhook"`
	Method        string `json:"method" jsonschema:"HTTP method the webhook's target endpoint expects: one of GET, POST, PUT, PATCH"`
	EventTypePath string `json:"event_type_path" jsonschema:"JSON path into the target table's row used to determine the event type"`
	EventIDPath   string `json:"event_id_path,omitempty" jsonschema:"optional JSON path used to deduplicate events"`
}

// orbitSaveWebhookEventMappingInput is the input for
// orbit_save_webhook_event_mapping. dashboard.EventMappingDef carries no
// json tags (internal store parameter struct), so a bespoke translation
// struct is needed here too.
type orbitSaveWebhookEventMappingInput struct {
	AppID          string                      `json:"app_id" jsonschema:"id of the app that owns the webhook"`
	WebhookID      string                      `json:"webhook_id" jsonschema:"id of the webhook to add the mapping to"`
	EventTypeValue string                      `json:"event_type_value" jsonschema:"the event type value this mapping applies to"`
	Action         string                      `json:"action" jsonschema:"one of insert, update, delete"`
	TargetTable    string                      `json:"target_table" jsonschema:"name of the app table this event writes to"`
	MatchKeyColumn string                      `json:"match_key_column,omitempty" jsonschema:"required when action is update or delete: the column used to find the target row"`
	FieldMappings  []dashboard.FieldMappingDef `json:"field_mappings" jsonschema:"how fields in the incoming event map to columns on the target table"`
}

// registerOperationalWriteTools registers the additive webhook mutation
// tools (mcp-safe-mutation-tools spec): create a webhook, save an event
// mapping. Both gate on role.CanManage() — a stricter tier than the table
// tools' CanWrite(), matching webhookRBACGate exactly (design.md's Tech
// Decisions: this is a real, confirmed tier difference, not an oversight).
func registerOperationalWriteTools(server *mcp.Server, deps ToolDeps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_create_webhook",
		Description: "Create a new webhook on an app, without affecting any other webhook on that app. Requires the caller to be able to manage the app (role.CanManage()). This tool's response never includes the webhook's callback token/URL — fetch it from the Dashboard (or the REST API) after creation to finish wiring up the external provider.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitCreateWebhookInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		row, err := deps.DashH.CreateWebhookForUser(ctx, user, in.AppID, dashboard.CreateWebhookInput{
			Name:          in.Name,
			Method:        in.Method,
			EventTypePath: in.EventTypePath,
			EventIDPath:   in.EventIDPath,
		}, "mcp")
		if err != nil {
			return nil, nil, mapWriteError(err)
		}
		return nil, toOrbitWebhookSummary(*row), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "orbit_save_webhook_event_mapping",
		Description: "Register (or replace, if the same event_type_value already exists and no conflicting mapping is present) an event-type-to-target-table mapping on a webhook, without affecting any other mapping. Requires the caller to be able to manage the app (role.CanManage()).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in orbitSaveWebhookEventMappingInput) (*mcp.CallToolResult, any, error) {
		user, ok := dashboard.UserFromContext(ctx)
		if !ok {
			return nil, nil, errUnauthorized
		}
		row, err := deps.DashH.SaveEventMappingForUser(ctx, user, in.AppID, in.WebhookID, dashboard.EventMappingDef{
			EventTypeValue: in.EventTypeValue,
			Action:         in.Action,
			TargetTable:    in.TargetTable,
			MatchKeyColumn: in.MatchKeyColumn,
			FieldMappings:  in.FieldMappings,
		}, "mcp")
		if err != nil {
			return nil, nil, mapWriteError(err)
		}
		return nil, toOrbitEventMapping(*row), nil
	})
}
