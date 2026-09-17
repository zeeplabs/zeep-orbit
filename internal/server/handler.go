package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/query"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// Handler holds dependencies for CRUD HTTP handlers.
type Handler struct {
	pool *db.Pool
	reg  *registry.Registry
}

// NewHandler creates a Handler with injected pool and registry.
func NewHandler(pool *db.Pool, reg *registry.Registry) *Handler {
	return &Handler{pool: pool, reg: reg}
}

// resolveOwner returns the owner_id value to write/filter with. It reports
// ok=false when the table needs an owner_id column but no authenticated user
// is in context. The returned ownerID is always the real user.ID for
// "owner"/"enabled"/"policy" tables (config.HasOwnerColumn) — callers that
// only want the automatic list/get/update/delete filter must additionally
// check config.AutoScopesByOwner(table.RLS) before passing this value to a
// query.Build* filter argument; query.BuildInsert always receives it as-is,
// since owner_id must be populated on every INSERT regardless of RLS mode.
func resolveOwner(ctx context.Context, table *registry.Table) (ownerID string, ok bool) {
	if !config.HasOwnerColumn(table.RLS) {
		return "", true
	}
	user, hasUser := auth.UserFromContext(ctx)
	if !hasUser {
		return "", false
	}
	return user.ID, true
}

// filterOwner returns ownerID unchanged when table.RLS auto-scopes by owner
// ("owner"/"enabled"), or "" otherwise — including "policy" mode, where
// visibility is left entirely to native Postgres table policies and no
// owner_id filter is ever applied by the application.
func filterOwner(ownerID string, table *registry.Table) string {
	if config.AutoScopesByOwner(table.RLS) {
		return ownerID
	}
	return ""
}

// rlsClaimsFromContext builds the claims WithRLSContext exposes as session
// GUCs for native Postgres row policies. Zero-value claims (no authenticated
// user in context) are safe: any policy comparing against role/sub/email
// simply won't match, which is default-deny, not a bypass.
func rlsClaimsFromContext(ctx context.Context) db.RLSClaims {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return db.RLSClaims{}
	}
	return db.RLSClaims{Role: user.Role, Sub: user.ID, Email: user.Email}
}

// checkViolationMessage builds a safe, non-leaking message for a Postgres
// check_violation (23514) error — e.g. an out-of-set write to an enum
// column's CHECK constraint. Never includes pgErr.Message/Detail, since
// those can echo back the attempted value.
func checkViolationMessage(pgErr *pgconn.PgError) string {
	if pgErr.ColumnName != "" {
		return fmt.Sprintf("value not allowed for column %q", pgErr.ColumnName)
	}
	if pgErr.ConstraintName != "" {
		return fmt.Sprintf("value violates constraint %q", pgErr.ConstraintName)
	}
	return "value violates a check constraint"
}

// SPEC_DEVIATION: insert-error-diagnostics INSERTERR-01 (classifying
// invalid_datetime_format/datetime_field_overflow/invalid_text_representation
// as 400 naming the column) is NOT implemented here as a pgErr.Code branch.
// Reason: pgErr.ColumnName is empty for a cast failure on a typed
// placeholder ($1::timestamptz) — Postgres doesn't attach column context to
// that error class, verified empirically. See query.BuildInsert's
// timestamptz validation instead, which catches this before the query ever
// reaches Postgres and can therefore name the column reliably.

// uniqueViolationMessage builds a safe, non-leaking message for a Postgres
// unique_violation (23505). Never includes pgErr.Message/Detail, since those
// can echo back the attempted value.
func uniqueViolationMessage(pgErr *pgconn.PgError) string {
	if pgErr.ConstraintName != "" {
		return fmt.Sprintf("row already exists: unique constraint %q", pgErr.ConstraintName)
	}
	return "row already exists"
}

// parseOnConflict extracts and validates the optional "on_conflict" /
// "conflict_columns" request fields, removing both from body so BuildInsert
// doesn't treat them as unknown table columns. onConflict defaults to
// "error" (today's behavior: a unique violation surfaces as an error)
// when the field is absent. conflict_columns is required (non-empty) when
// onConflict is "update" — Postgres's ON CONFLICT DO UPDATE needs an
// explicit conflict target, and the registry only models single-column
// uniques, not the composite ones this feature was written for — so the
// caller must supply the target explicitly rather than have it inferred.
func parseOnConflict(body map[string]any, table *registry.Table) (onConflict string, conflictColumns []string, err error) {
	onConflict, _ = body["on_conflict"].(string)
	delete(body, "on_conflict")

	if raw, present := body["conflict_columns"]; present {
		delete(body, "conflict_columns")
		arr, ok := raw.([]any)
		if !ok {
			return "", nil, fmt.Errorf("conflict_columns must be an array of column names")
		}
		seen := make(map[string]struct{}, len(arr))
		for _, v := range arr {
			s, ok := v.(string)
			if !ok {
				return "", nil, fmt.Errorf("conflict_columns must be an array of column names")
			}
			if _, dup := seen[s]; dup {
				return "", nil, fmt.Errorf("duplicate column in conflict_columns: %q", s)
			}
			seen[s] = struct{}{}
			conflictColumns = append(conflictColumns, s)
		}
	}

	switch onConflict {
	case "", "error", "ignore", "update":
	default:
		return "", nil, fmt.Errorf("invalid on_conflict value %q", onConflict)
	}

	if onConflict == "update" && len(conflictColumns) == 0 {
		return "", nil, fmt.Errorf("on_conflict \"update\" requires a non-empty conflict_columns")
	}

	if len(conflictColumns) > 0 {
		known := make(map[string]struct{}, len(table.Columns))
		for _, col := range table.Columns {
			known[col.Name] = struct{}{}
		}
		for _, c := range conflictColumns {
			if _, ok := known[c]; !ok {
				return "", nil, fmt.Errorf("unknown column in conflict_columns: %q", c)
			}
			// A conflict_columns value that's absent from the body would
			// build "col = NULL" in fetchRowByColumns' WHERE clause (an
			// always-false match, since "=" with NULL is never true) —
			// that silently misreports a genuine caller mistake as 409
			// "row already exists" instead of naming the real problem.
			if _, present := body[c]; !present {
				return "", nil, fmt.Errorf("conflict_columns column %q must be present in the request body", c)
			}
		}
	}

	return onConflict, conflictColumns, nil
}

// Response: {"data": [...], "count": N, "limit": L, "offset": O}
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	tableName := chi.URLParam(r, "table")

	table, ok := app.Tables[tableName]
	if !ok {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	ownerID, ok := resolveOwner(r.Context(), table)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	params := make(map[string]string)
	for k, vals := range r.URL.Query() {
		if len(vals) > 0 {
			params[k] = vals[0]
		}
	}

	q, err := query.BuildList(app.SchemaName, tableName, table, params, filterOwner(ownerID, table), h.reg.SystemConfig().SoftDeleteEnabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	filterArgs := q.Args[:len(q.Args)-2]

	var count int
	var data []map[string]any
	err = h.pool.WithRLSContext(ctx, rlsClaimsFromContext(ctx), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		if err := qx.QueryRow(ctx, q.CountSQL, filterArgs...).Scan(&count); err != nil {
			return err
		}
		rows, err := qx.Query(ctx, q.SQL, q.Args...)
		if err != nil {
			return err
		}
		data, err = pgx.CollectRows(rows, pgx.RowToMap)
		return err
	})
	if err != nil {
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to query rows")
		return
	}
	if data == nil {
		data = []map[string]any{}
	}

	limit := q.Args[len(q.Args)-2]
	offset := q.Args[len(q.Args)-1]

	writeJSON(w, http.StatusOK, map[string]any{
		"data":   sanitizeRows(data),
		"count":  count,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	tableName := chi.URLParam(r, "table")

	table, ok := app.Tables[tableName]
	if !ok {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	ownerID, ok := resolveOwner(r.Context(), table)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	onConflict, conflictColumns, err := parseOnConflict(body, table)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	q, err := query.BuildInsertWithOptions(app.SchemaName, tableName, table, body, ownerID, query.InsertOptions{
		OnConflict:          onConflict,
		ConflictColumns:     conflictColumns,
		ConflictOwnerFilter: filterOwner(ownerID, table),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var row map[string]any
	var existing map[string]any
	var noRowsFromConflict bool
	err = h.pool.WithRLSContext(r.Context(), rlsClaimsFromContext(r.Context()), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		rows, err := qx.Query(r.Context(), q.SQL, q.Args...)
		if err != nil {
			return err
		}
		row, err = pgx.CollectOneRow(rows, pgx.RowToMap)
		if onConflict != "ignore" || !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		noRowsFromConflict = true
		if len(conflictColumns) == 0 {
			return nil
		}
		// Runs in the same transaction as the INSERT above — fetching the
		// pre-existing row via a separate WithRLSContext call (a second,
		// later transaction) left a window for the row to be deleted or
		// reassigned between the two, and for a soft-deleted row's
		// deleted_at to be seen inconsistently. A single transaction removes
		// that window entirely.
		existing, err = h.fetchRowByColumns(r.Context(), qx, app.SchemaName, tableName, table, conflictColumns, body, filterOwner(ownerID, table), h.reg.SystemConfig().SoftDeleteEnabled)
		return err
	})
	if err != nil {
		if noRowsFromConflict {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusConflict, "row already exists")
				return
			}
			if db.IsStatementTimeout(err) {
				writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to fetch existing row")
			return
		}
		// on_conflict:"update" adds a WHERE owner_id = $n guard to the DO
		// UPDATE on owner-scoped tables (query.InsertOptions.ConflictOwnerFilter)
		// — a conflict belonging to another tenant is neither updated nor
		// inserted, so RETURNING produces zero rows here instead of leaking or
		// overwriting a row the caller doesn't own.
		if onConflict == "update" && errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "row already exists")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			writeError(w, http.StatusBadRequest, checkViolationMessage(pgErr))
			return
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, uniqueViolationMessage(pgErr))
			return
		}
		if errors.As(err, &pgErr) && pgErr.Code == "42P10" {
			writeError(w, http.StatusBadRequest, "conflict_columns does not match any unique or exclusion constraint on this table")
			return
		}
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to insert row")
		return
	}

	if noRowsFromConflict {
		if len(conflictColumns) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, sanitizeRow(existing))
		return
	}

	if onConflict == "update" {
		// zeep_was_insert (from BuildInsertWithOptions's RETURNING *,
		// (xmax = 0) AS zeep_was_insert) distinguishes a genuine first-time
		// insert from a real upsert-over-a-conflict — both take the
		// on_conflict:"update" path, but reporting 200 for a fresh row would
		// misreport creation to any client keying off status code.
		wasInsert, _ := row["zeep_was_insert"].(bool)
		delete(row, "zeep_was_insert")
		status := http.StatusOK
		if wasInsert {
			status = http.StatusCreated
		}
		writeJSON(w, status, sanitizeRow(row))
		return
	}

	writeJSON(w, http.StatusCreated, sanitizeRow(row))
}

// fetchRowByColumns runs a single-row SELECT matching each name in columns
// to its value in body — used only to fetch the pre-existing row after an
// on_conflict:"ignore" insert short-circuits with zero rows via DO NOTHING.
// Always called with the same qx (Querier/transaction) as that INSERT, so
// the two run atomically — a separate transaction here would let the row be
// deleted or its deleted_at change between the two statements.
//
// ownerFilter must be the caller's filterOwner(ownerID, table) result, never
// a raw ownerID — omitting this filter let any caller fetch any other
// tenant's row by guessing/brute-forcing a unique column's value, since
// "owner"/"enabled" RLS modes have no native Postgres policy backing them;
// the app-level predicate here is the only enforcement. softDelete excludes
// soft-deleted rows the same way BuildList/BuildDelete do.
func (h *Handler) fetchRowByColumns(ctx context.Context, qx db.Querier, schemaName, tableName string, table *registry.Table, columns []string, body map[string]any, ownerFilter string, softDelete bool) (map[string]any, error) {
	colByName := make(map[string]registry.Column, len(table.Columns))
	types := make(map[string]string, len(table.Columns))
	for _, col := range table.Columns {
		colByName[col.Name] = col
		types[col.Name] = col.Type
	}

	var whereClauses []string
	var args []any
	for _, c := range columns {
		val := body[c]
		if types[c] == "timestamptz" {
			// Mirrors the normalization BuildInsertWithOptions applied to
			// this same value before the INSERT — body isn't mutated there,
			// so without this an "" conflict_columns value (valid, and
			// normalized to NULL for the INSERT) would reach this SELECT as
			// a raw ""::timestamptz cast, failing with an unclassified 500
			// instead of matching the row the INSERT just conflicted with.
			var err error
			val, err = query.NormalizeTimestamptz(colByName[c], val)
			if err != nil {
				return nil, err
			}
		}
		args = append(args, val)
		whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d%s", c, len(args), query.PgCast(types[c])))
	}
	if ownerFilter != "" {
		args = append(args, ownerFilter)
		whereClauses = append(whereClauses, fmt.Sprintf("owner_id = $%d::uuid", len(args)))
	}
	if softDelete {
		whereClauses = append(whereClauses, "deleted_at IS NULL")
	}
	sql := fmt.Sprintf("SELECT * FROM %s.%s WHERE %s", schemaName, tableName, strings.Join(whereClauses, " AND "))

	rows, err := qx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToMap)
}

// 404 {"error":"not found"} if not found.
func (h *Handler) HandleGetByID(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	tableName := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")

	table, ok := app.Tables[tableName]
	if !ok {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	ownerID, ok := resolveOwner(r.Context(), table)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	q := query.BuildGetByID(app.SchemaName, tableName, id, filterOwner(ownerID, table))

	var row map[string]any
	err := h.pool.WithRLSContext(r.Context(), rlsClaimsFromContext(r.Context()), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		rows, err := qx.Query(r.Context(), q.SQL, q.Args...)
		if err != nil {
			return err
		}
		row, err = pgx.CollectOneRow(rows, pgx.RowToMap)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to query row")
		return
	}

	writeJSON(w, http.StatusOK, sanitizeRow(row))
}

// 404 if not found.
func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	tableName := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")

	table, ok := app.Tables[tableName]
	if !ok {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	ownerID, ok := resolveOwner(r.Context(), table)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	q, err := query.BuildUpdate(app.SchemaName, tableName, table, id, body, filterOwner(ownerID, table))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var row map[string]any
	err = h.pool.WithRLSContext(r.Context(), rlsClaimsFromContext(r.Context()), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		rows, err := qx.Query(r.Context(), q.SQL, q.Args...)
		if err != nil {
			return err
		}
		row, err = pgx.CollectOneRow(rows, pgx.RowToMap)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			writeError(w, http.StatusBadRequest, checkViolationMessage(pgErr))
			return
		}
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update row")
		return
	}

	writeJSON(w, http.StatusOK, sanitizeRow(row))
}

// 404 if not found.
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	tableName := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")

	table, ok := app.Tables[tableName]
	if !ok {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	ownerID, ok := resolveOwner(r.Context(), table)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	q := query.BuildDelete(app.SchemaName, tableName, id, filterOwner(ownerID, table), h.reg.SystemConfig().SoftDeleteEnabled)

	var affected int64
	err := h.pool.WithRLSContext(r.Context(), rlsClaimsFromContext(r.Context()), h.reg.SystemConfig().StatementTimeoutMs, func(qx db.Querier) error {
		tag, err := qx.Exec(r.Context(), q.SQL, q.Args...)
		if err != nil {
			return err
		}
		affected = tag.RowsAffected()
		return nil
	})
	if err != nil {
		if db.IsStatementTimeout(err) {
			writeError(w, http.StatusServiceUnavailable, "query exceeded statement timeout")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete row")
		return
	}

	if affected == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleHealth implementa GET /health → {"status":"ok","apps":N}.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	apps := h.reg.Apps()
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"apps":   len(apps),
	})
}

func (h *Handler) HandleAppHealth(w http.ResponseWriter, r *http.Request) {
	appName := chi.URLParam(r, "app")

	app, ok := h.reg.Get(appName)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "not_found", "error": "app not found"})
		return
	}

	dbOK := true
	if err := h.pool.Ping(r.Context()); err != nil {
		dbOK = false
	}

	schemaOK := true
	if dbOK {
		var exists bool
		err := h.pool.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`,
			app.SchemaName,
		).Scan(&exists)
		if err != nil || !exists {
			schemaOK = false
		}
	}

	healthy := dbOK && schemaOK
	code := http.StatusOK
	if !healthy {
		code = http.StatusServiceUnavailable
	}

	writeJSON(w, code, map[string]any{
		"status":  "ok",
		"app":     appName,
		"healthy": healthy,
		"checks": map[string]bool{
			"database": dbOK,
			"schema":   schemaOK,
		},
	})
}
