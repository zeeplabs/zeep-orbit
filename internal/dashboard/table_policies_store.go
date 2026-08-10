package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// isDuplicatePolicyName reports whether err is Postgres's own
// duplicate_object error (SQLSTATE 42710) — raised by CREATE POLICY when a
// policy with the same name already exists on the table, regardless of
// action.
func isDuplicatePolicyName(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42710"
	}
	return false
}

// ErrPolicyAlreadyExists is returned when the (app_id, table_name, action,
// pg_policy_name) UNIQUE constraint is violated — mapped by the handler to
// HTTP 409 (spec Edge Cases: duplicate policy name on the same table/action).
var ErrPolicyAlreadyExists = errors.New("dashboard: policy already exists")

// PolicyClause is the persisted, JSON-serializable form of one structured
// row-policy condition. Same field-for-field shape as provisioner.PolicyClause
// (which does the actual SQL translation) but kept as a separate type here:
// this one carries json tags for the API/DB boundary, the provisioner one is
// internal to DDL generation — the store converts between them.
type PolicyClause struct {
	Column      string `json:"column"`
	Operator    string `json:"operator"`
	ValueSource string `json:"value_source,omitempty"`
	Value       string `json:"value,omitempty"`
	Logic       string `json:"logic,omitempty"`
}

// PolicyDef is the create-policy request payload. Name becomes the real
// Postgres policy name (CREATE POLICY "<name>" ...) — it is admin-supplied
// (not auto-generated) so the duplicate-name-on-same-table/action edge case
// in the spec is meaningful (two policies submitted with the same Name
// collide on the UNIQUE constraint and surface as 409, not silently
// succeed with two indistinguishable auto-generated names).
type PolicyDef struct {
	Name    string         `json:"name"`
	Action  string         `json:"action"`
	Roles   []string       `json:"roles"`
	Clauses []PolicyClause `json:"clauses"`
}

// TablePolicyRow is a row from zeep_system.table_policies.
type TablePolicyRow struct {
	ID           string         `json:"id"`
	TableName    string         `json:"table_name"`
	Action       string         `json:"action"`
	Roles        []string       `json:"roles"`
	Clauses      []PolicyClause `json:"clauses"`
	PgPolicyName string         `json:"pg_policy_name"`
	CreatedAt    time.Time      `json:"created_at"`
	CreatedBy    string         `json:"created_by"`
}

// toProvisionerClauses converts the persisted clause shape into the shape
// provisioner.BuildPolicySQL consumes.
func toProvisionerClauses(clauses []PolicyClause) []provisioner.PolicyClause {
	out := make([]provisioner.PolicyClause, len(clauses))
	for i, c := range clauses {
		out[i] = provisioner.PolicyClause{
			Column:      c.Column,
			Operator:    c.Operator,
			ValueSource: c.ValueSource,
			Value:       c.Value,
			Logic:       c.Logic,
		}
	}
	return out
}

// CreateTablePolicy validates def via provisioner.BuildPolicySQL, enables
// native RLS on the table the first time any policy is created for it
// (idempotent — never disabled again, per spec default-deny requirement),
// executes the generated CREATE POLICY, and persists the metadata row —
// all inside one transaction so a DDL failure never leaves an orphaned
// metadata row (and vice versa).
func CreateTablePolicy(ctx context.Context, pool *db.Pool, appID, schemaName, tableName string, tableColumns []config.ColumnConfig, def PolicyDef, createdBy string) (TablePolicyRow, error) {
	ddl, err := provisioner.BuildPolicySQL(schemaName, tableName, provisioner.PolicyDef{
		Name:    def.Name,
		Action:  def.Action,
		Roles:   def.Roles,
		Clauses: toProvisionerClauses(def.Clauses),
	}, tableColumns)
	if err != nil {
		return TablePolicyRow{}, err
	}

	rolesJSON, err := json.Marshal(def.Roles)
	if err != nil {
		return TablePolicyRow{}, fmt.Errorf("dashboard: marshal policy roles: %w", err)
	}
	clausesJSON, err := json.Marshal(def.Clauses)
	if err != nil {
		return TablePolicyRow{}, fmt.Errorf("dashboard: marshal policy clauses: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return TablePolicyRow{}, fmt.Errorf("dashboard: create policy begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var existingCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.table_policies WHERE app_id = $1 AND table_name = $2`,
		appID, tableName,
	).Scan(&existingCount); err != nil {
		return TablePolicyRow{}, fmt.Errorf("dashboard: create policy count existing: %w", err)
	}
	if existingCount == 0 {
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`ALTER TABLE %q.%q ENABLE ROW LEVEL SECURITY`, schemaName, tableName),
		); err != nil {
			return TablePolicyRow{}, fmt.Errorf("dashboard: enable row level security on %q.%q: %w", schemaName, tableName, err)
		}
	}

	if _, err := tx.Exec(ctx, ddl); err != nil {
		// Postgres itself enforces a unique policy name per table (SQLSTATE
		// 42710 duplicate_object) independent of our own UNIQUE constraint on
		// the metadata row below — a name reused across two different
		// actions on the same table hits this native check first, before the
		// INSERT below ever runs.
		if isDuplicatePolicyName(err) {
			return TablePolicyRow{}, ErrPolicyAlreadyExists
		}
		return TablePolicyRow{}, fmt.Errorf("dashboard: create policy DDL: %w", err)
	}

	var row TablePolicyRow
	err = tx.QueryRow(ctx,
		`INSERT INTO zeep_system.table_policies (app_id, table_name, action, roles, clauses, pg_policy_name, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, table_name, action, pg_policy_name, created_at, created_by`,
		appID, tableName, def.Action, rolesJSON, clausesJSON, def.Name, createdBy,
	).Scan(&row.ID, &row.TableName, &row.Action, &row.PgPolicyName, &row.CreatedAt, &row.CreatedBy)
	if err != nil {
		if isUniqueViolation(err) {
			return TablePolicyRow{}, ErrPolicyAlreadyExists
		}
		return TablePolicyRow{}, fmt.Errorf("dashboard: insert table policy: %w", err)
	}
	row.Roles = def.Roles
	row.Clauses = def.Clauses

	if err := tx.Commit(ctx); err != nil {
		return TablePolicyRow{}, fmt.Errorf("dashboard: create policy commit: %w", err)
	}
	return row, nil
}

// ListTablePolicies returns every policy registered for a table, newest first.
func ListTablePolicies(ctx context.Context, pool *db.Pool, appID, tableName string) ([]TablePolicyRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, table_name, action, roles, clauses, pg_policy_name, created_at, created_by
		 FROM zeep_system.table_policies
		 WHERE app_id = $1 AND table_name = $2
		 ORDER BY created_at DESC`,
		appID, tableName,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list table policies: %w", err)
	}
	defer rows.Close()

	result := make([]TablePolicyRow, 0)
	for rows.Next() {
		var (
			row                TablePolicyRow
			rolesJSON, clsJSON []byte
		)
		if err := rows.Scan(&row.ID, &row.TableName, &row.Action, &rolesJSON, &clsJSON, &row.PgPolicyName, &row.CreatedAt, &row.CreatedBy); err != nil {
			return nil, fmt.Errorf("dashboard: scan table policy row: %w", err)
		}
		if err := json.Unmarshal(rolesJSON, &row.Roles); err != nil {
			return nil, fmt.Errorf("dashboard: unmarshal policy roles: %w", err)
		}
		if err := json.Unmarshal(clsJSON, &row.Clauses); err != nil {
			return nil, fmt.Errorf("dashboard: unmarshal policy clauses: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// DeleteTablePolicy removes a policy's metadata row and executes the
// corresponding DROP POLICY. It never disables ROW LEVEL SECURITY on the
// table, even when it deletes the last policy for that table/action — the
// spec requires default-deny to remain explicit (a table with RLS enabled
// and zero policies denies every row to zeep_app_enduser, which is the
// intended behavior, not a bug to "fix" by disabling RLS again).
func DeleteTablePolicy(ctx context.Context, pool *db.Pool, appID, schemaName, policyID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dashboard: delete policy begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var tableName, pgPolicyName string
	err = tx.QueryRow(ctx,
		`DELETE FROM zeep_system.table_policies WHERE id = $1 AND app_id = $2 RETURNING table_name, pg_policy_name`,
		policyID, appID,
	).Scan(&tableName, &pgPolicyName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("dashboard: delete table policy %s: %w", policyID, err)
	}

	if _, err := tx.Exec(ctx,
		fmt.Sprintf(`DROP POLICY IF EXISTS %q ON %q.%q`, pgPolicyName, schemaName, tableName),
	); err != nil {
		return fmt.Errorf("dashboard: drop policy %q on %q.%q: %w", pgPolicyName, schemaName, tableName, err)
	}

	return tx.Commit(ctx)
}

// deleteTablePoliciesForTable deletes every table_policies row for one
// table's metadata (called when the table itself is deleted via
// DeleteAppTable). table_policies has no DB-level FK to app_tables (its FK
// is to apps, for the "whole app deleted" case) — table_name is a plain
// column resolved logically the same way app_tables.name is, so this
// cleanup is done at the application level, matching the spec edge case
// ("o DROP TABLE já remove as policies nativas do Postgres; a limpeza é do
// metadado próprio do Orbit"). Native policies are already gone once DROP
// TABLE runs; this only removes the now-orphaned Orbit-side metadata rows.
func deleteTablePoliciesForTable(ctx context.Context, pool *db.Pool, appID, tableName string) error {
	if _, err := pool.Exec(ctx,
		`DELETE FROM zeep_system.table_policies WHERE app_id = $1 AND table_name = $2`,
		appID, tableName,
	); err != nil {
		return fmt.Errorf("dashboard: delete table policies for table %q: %w", tableName, err)
	}
	return nil
}
