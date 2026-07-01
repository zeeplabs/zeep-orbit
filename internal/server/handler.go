package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/auth"
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

// Returns ok=false when RLS is enabled but no authenticated user is in context.
func resolveOwner(ctx context.Context, table *registry.Table) (ownerID string, ok bool) {
	if table.RLS != "owner" && table.RLS != "enabled" {
		return "", true
	}
	user, hasUser := auth.UserFromContext(ctx)
	if !hasUser {
		return "", false
	}
	return user.ID, true
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

	q, err := query.BuildList(app.SchemaName, tableName, table, params, ownerID, h.reg.SystemConfig().SoftDeleteEnabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	// COUNT
	var count int
	filterArgs := q.Args[:len(q.Args)-2]
	if err := h.pool.QueryRow(ctx, q.CountSQL, filterArgs...).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count rows")
		return
	}

	rows, err := h.pool.Query(ctx, q.SQL, q.Args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query rows")
		return
	}
	data, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to collect rows")
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

// HandleCreate implementa POST /{app}/{table} → 201 + row criada.
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

	q, err := query.BuildInsert(app.SchemaName, tableName, table, body, ownerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := h.pool.Query(r.Context(), q.SQL, q.Args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert row")
		return
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to collect inserted row: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, sanitizeRow(row))
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

	q := query.BuildGetByID(app.SchemaName, tableName, id, ownerID)

	rows, err := h.pool.Query(r.Context(), q.SQL, q.Args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query row")
		return
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to collect row")
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

	q, err := query.BuildUpdate(app.SchemaName, tableName, table, id, body, ownerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := h.pool.Query(r.Context(), q.SQL, q.Args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update row")
		return
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to collect updated row")
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

	q := query.BuildDelete(app.SchemaName, tableName, id, ownerID, h.reg.SystemConfig().SoftDeleteEnabled)

	tag, err := h.pool.Exec(r.Context(), q.SQL, q.Args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete row")
		return
	}

	if tag.RowsAffected() == 0 {
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

// HandleAppHealth implementa GET /{app}/health → status do app individual.
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
