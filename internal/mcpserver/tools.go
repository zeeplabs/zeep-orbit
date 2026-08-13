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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
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
