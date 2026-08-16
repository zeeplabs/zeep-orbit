package dashboard

import (
	"context"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ListAppsForUser is a thin extraction of the same query ListApps already
// runs — kept as its own name (mirroring GetAppSchemaForUser below) so the
// MCP tool registry (orbit_list_apps, mcp-server spec, T10) has a stable
// name to wrap, even though it does not add any new logic.
func ListAppsForUser(ctx context.Context, pool *db.Pool, user *DashboardUser) ([]*AppRow, error) {
	return ListApps(ctx, pool, user)
}

// AppSchemaColumn is one column in AppSchemaTable.Columns.
type AppSchemaColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// AppSchemaPolicy is one row policy in AppSchemaTable.Policies.
type AppSchemaPolicy struct {
	Name   string   `json:"name"`
	Action string   `json:"action"`
	Roles  []string `json:"roles"`
}

// AppSchemaTable is one table in AppSchema.Tables.
type AppSchemaTable struct {
	Name     string            `json:"name"`
	RLSMode  string            `json:"rls_mode"`
	Columns  []AppSchemaColumn `json:"columns"`
	Policies []AppSchemaPolicy `json:"policies"`
}

// AppSchema is the response shape for orbit_get_app_schema (mcp-server
// spec, MCP-09) — genuinely new aggregation logic, not a pure extraction
// (design.md Tech Decisions): no existing single endpoint returns tables +
// columns + rls_mode + policies together. Assembled from the same store
// reads GetApp/ListTablePolicies already use elsewhere.
type AppSchema struct {
	AppID   string           `json:"app_id"`
	AppName string           `json:"app_name"`
	Tables  []AppSchemaTable `json:"tables"`
}

// GetAppSchemaForUser assembles an app's current tables/columns/RLS modes/
// policies, so an LLM driving MCP tools can verify state after each step
// without re-deriving it from prior tool responses alone. Authorization is
// identical to GetApp's — the same ErrNotFound both apps-that-don't-exist
// and apps-the-user-can't-see already return there.
func GetAppSchemaForUser(ctx context.Context, pool *db.Pool, user *DashboardUser, appID string) (*AppSchema, error) {
	app, _, err := GetApp(ctx, pool, appID, user)
	if err != nil {
		return nil, err
	}

	tables := make([]AppSchemaTable, 0, len(app.Tables))
	for _, t := range app.Tables {
		policies, err := ListTablePolicies(ctx, pool, appID, t.Name)
		if err != nil {
			return nil, err
		}

		policyOut := make([]AppSchemaPolicy, 0, len(policies))
		for _, p := range policies {
			policyOut = append(policyOut, AppSchemaPolicy{Name: p.PgPolicyName, Action: p.Action, Roles: p.Roles})
		}

		colsOut := make([]AppSchemaColumn, 0, len(t.Columns))
		for _, c := range t.Columns {
			colsOut = append(colsOut, AppSchemaColumn{Name: c.Name, Type: c.Type, Nullable: !c.Required})
		}

		tables = append(tables, AppSchemaTable{
			Name:     t.Name,
			RLSMode:  t.RLS,
			Columns:  colsOut,
			Policies: policyOut,
		})
	}

	return &AppSchema{
		AppID:   app.ID,
		AppName: app.Name,
		Tables:  tables,
	}, nil
}
