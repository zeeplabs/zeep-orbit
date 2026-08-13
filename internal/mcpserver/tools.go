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

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ToolDeps bundles what every registered tool needs — currently just the
// shared Postgres pool the *ForUser operation functions already take. Kept
// as its own type so RegisterTools's signature doesn't need to change if a
// future tool needs another dependency.
type ToolDeps struct {
	Pool *db.Pool
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
