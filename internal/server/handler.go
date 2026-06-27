package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/zeep-tecnologia/zeep-core/internal/db"
	"github.com/zeep-tecnologia/zeep-core/internal/query"
	"github.com/zeep-tecnologia/zeep-core/internal/registry"
)

// Handler encapsula as dependências dos CRUD handlers HTTP.
type Handler struct {
	pool *db.Pool
	reg  *registry.Registry
}

// NewHandler cria um Handler com pool e registry injetados.
func NewHandler(pool *db.Pool, reg *registry.Registry) *Handler {
	return &Handler{pool: pool, reg: reg}
}

// HandleList implementa GET /{app}/{table}.
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

	// Converter query params para map[string]string
	params := make(map[string]string)
	for k, vals := range r.URL.Query() {
		if len(vals) > 0 {
			params[k] = vals[0]
		}
	}

	q, err := query.BuildList(app.SchemaName, tableName, table, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	// COUNT
	var count int
	// CountSQL usa apenas os filtros (sem LIMIT/OFFSET), então passamos só os args de filtro
	// Os últimos 2 args de q.Args são limit e offset — CountSQL não usa placeholders para eles
	filterArgs := q.Args[:len(q.Args)-2]
	if err := h.pool.QueryRow(ctx, q.CountSQL, filterArgs...).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count rows")
		return
	}

	// DATA
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

	// Extrair limit e offset dos args (últimas 2 posições)
	limit := q.Args[len(q.Args)-2]
	offset := q.Args[len(q.Args)-1]

	writeJSON(w, http.StatusOK, map[string]any{
		"data":   data,
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

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	q, err := query.BuildInsert(app.SchemaName, tableName, table, body)
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
		writeError(w, http.StatusInternalServerError, "failed to collect inserted row")
		return
	}

	writeJSON(w, http.StatusCreated, row)
}

// HandleGetByID implementa GET /{app}/{table}/{id}.
// 404 {"error":"not found"} se não existe.
func (h *Handler) HandleGetByID(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	tableName := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")

	if _, ok := app.Tables[tableName]; !ok {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	q := query.BuildGetByID(app.SchemaName, tableName)

	rows, err := h.pool.Query(r.Context(), q.SQL, id)
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

	writeJSON(w, http.StatusOK, row)
}

// HandleUpdate implementa PATCH /{app}/{table}/{id} (parcial).
// 404 se não existe.
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

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	q, err := query.BuildUpdate(app.SchemaName, tableName, table, id, body)
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

	writeJSON(w, http.StatusOK, row)
}

// HandleDelete implementa DELETE /{app}/{table}/{id} → 204 No Content.
// 404 se não existe.
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	app, ok := AppFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "app not in context")
		return
	}

	tableName := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")

	if _, ok := app.Tables[tableName]; !ok {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	q := query.BuildDelete(app.SchemaName, tableName)

	tag, err := h.pool.Exec(r.Context(), q.SQL, id)
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
