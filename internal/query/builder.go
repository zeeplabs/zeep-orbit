package query

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// ListQuery is the result of BuildList: paginated SQL + unpaginated CountSQL.
type ListQuery struct {
	SQL      string
	Args     []any
	CountSQL string
}

// WriteQuery is the result of write operations (INSERT, UPDATE, DELETE, GET by ID).
type WriteQuery struct {
	SQL  string
	Args []any
}

// columnSet retorna um conjunto (map) de nomes de colunas conhecidas para lookup O(1).
func columnSet(table *registry.Table) map[string]struct{} {
	set := make(map[string]struct{}, len(table.Columns)+len(systemFields))
	for col := range systemFields {
		set[col] = struct{}{}
	}
	for _, col := range table.Columns {
		set[col.Name] = struct{}{}
	}
	return set
}

// columnTypes returns map[colName]colType for fast type lookup.
func columnTypes(table *registry.Table) map[string]string {
	m := make(map[string]string, len(table.Columns)+len(systemFields))
	for col, typ := range systemFields {
		m[col] = typ
	}
	for _, col := range table.Columns {
		m[col.Name] = col.Type
	}
	return m
}

// uuid and timestamptz do not have auto-cast from text in pgx extended protocol.
func pgCast(colType string) string {
	switch colType {
	case "uuid":
		return "::uuid"
	case "timestamptz":
		return "::timestamptz"
	default:
		return ""
	}
}

// systemFields are fields managed by the server that must never come from the caller.
var systemFields = map[string]string{
	"id":         "uuid",
	"created_at": "timestamptz",
	"updated_at": "timestamptz",
	"owner_id":   "uuid",
	"deleted_at": "timestamptz",
}

// operatorMap mapeia prefixo de operador → SQL operator.
var operatorMap = map[string]string{
	"eq.":   "=",
	"ne.":   "!=",
	"gt.":   ">",
	"gte.":  ">=",
	"lt.":   "<",
	"lte.":  "<=",
	"like.": "LIKE",
	"ilike.": "ILIKE",
}

//   - order={field}.asc|{field}.desc: sorting; field must exist in the table
//   - softDelete=true: automatically filters WHERE deleted_at IS NULL (unless ?deleted=true)
func BuildList(schemaName, tableName string, table *registry.Table, params map[string]string, ownerID string, softDelete bool) (*ListQuery, error) {
	const defaultLimit = 50
	const maxLimit = 1000

	known := columnSet(table)
	types := columnTypes(table)

	limit := defaultLimit
	if raw, ok := params["limit"]; ok {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("query: parâmetro 'limit' inválido: %q", raw)
		}
		if v > maxLimit {
			v = maxLimit
		}
		limit = v
	}

	offset := 0
	if raw, ok := params["offset"]; ok {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("query: parâmetro 'offset' inválido: %q", raw)
		}
		offset = v
	}

	// --- filtros (field=eq.value) ---
	var whereClauses []string
	var args []any

	showDeleted := false
	for key, val := range params {
		if key == "limit" || key == "offset" || key == "order" {
			continue
		}
		if key == "deleted" && val == "true" {
			showDeleted = true
			continue
		}
		if _, ok := known[key]; !ok {
			return nil, fmt.Errorf("query: campo desconhecido no filtro: %q", key)
		}

		if strings.HasPrefix(val, "in.") {
			raw := strings.TrimPrefix(val, "in.")
			parts := strings.Split(raw, ",")
			var placeholders []string
			for _, p := range parts {
				args = append(args, p)
				placeholders = append(placeholders, fmt.Sprintf("$%d%s", len(args), pgCast(types[key])))
			}
			whereClauses = append(whereClauses, fmt.Sprintf("%s IN (%s)", key, strings.Join(placeholders, ", ")))
			continue
		}

		op, found := "", false
		for prefix, sqlOp := range operatorMap {
			if strings.HasPrefix(val, prefix) {
				op = sqlOp
				filterVal := strings.TrimPrefix(val, prefix)
				args = append(args, filterVal)
				whereClauses = append(whereClauses, fmt.Sprintf("%s %s $%d%s", key, op, len(args), pgCast(types[key])))
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("query: operador não suportado em filtro '%s=%s' (use eq./ne./gt./gte./lt./lte./like./ilike./in.)", key, val)
		}
	}

	if ownerID != "" {
		args = append(args, ownerID)
		whereClauses = append(whereClauses, fmt.Sprintf("owner_id = $%d::uuid", len(args)))
	}

	if softDelete && !showDeleted {
		whereClauses = append(whereClauses, "deleted_at IS NULL")
	}

	// --- order ---
	var orderClause string
	if raw, ok := params["order"]; ok {
		parts := strings.Split(raw, ".")
		if len(parts) != 2 {
			return nil, fmt.Errorf("query: formato de 'order' inválido: %q (use field.asc ou field.desc)", raw)
		}
		field, direction := parts[0], strings.ToUpper(parts[1])
		if direction != "ASC" && direction != "DESC" {
			return nil, fmt.Errorf("query: direção de ordenação inválida: %q (use asc ou desc)", parts[1])
		}
		if _, ok := known[field]; !ok {
			return nil, fmt.Errorf("query: campo desconhecido em 'order': %q", field)
		}
		orderClause = fmt.Sprintf(" ORDER BY %s %s", field, direction)
	}

	baseFrom := fmt.Sprintf("FROM %s.%s", schemaName, tableName)
	var whereStr string
	if len(whereClauses) > 0 {
		whereStr = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	countSQL := fmt.Sprintf("SELECT COUNT(*) %s%s", baseFrom, whereStr)

	nextIdx := len(args) + 1
	listSQL := fmt.Sprintf("SELECT * %s%s%s LIMIT $%d OFFSET $%d",
		baseFrom, whereStr, orderClause, nextIdx, nextIdx+1)

	args = append(args, limit, offset)

	return &ListQuery{
		SQL:      listSQL,
		Args:     args,
		CountSQL: countSQL,
	}, nil
}

// Campos desconhecidos retornam erro.
func BuildInsert(schemaName, tableName string, table *registry.Table, body map[string]any, ownerID string) (*WriteQuery, error) {
	known := columnSet(table)
	types := columnTypes(table)

	for key := range body {
		if _, skip := systemFields[key]; skip {
			continue
		}
		if _, ok := known[key]; !ok {
			return nil, fmt.Errorf("query: campo desconhecido no body: %q", key)
		}
	}

	for _, col := range table.Columns {
		if !col.Required {
			continue
		}
		val, present := body[col.Name]
		if !present || val == nil {
			return nil, fmt.Errorf("query: campo %q é obrigatório", col.Name)
		}
	}

	// Builds column and placeholder lists in deterministic table column order
	var cols []string
	var placeholders []string
	var args []any

	for _, col := range table.Columns {
		if _, skip := systemFields[col.Name]; skip {
			continue
		}
		val, present := body[col.Name]
		if !present {
			continue
		}
		cols = append(cols, col.Name)
		args = append(args, val)
		placeholders = append(placeholders, fmt.Sprintf("$%d%s", len(args), pgCast(types[col.Name])))
	}

	if ownerID != "" {
		cols = append(cols, "owner_id")
		args = append(args, ownerID)
		placeholders = append(placeholders, fmt.Sprintf("$%d::uuid", len(args)))
	}

	if len(cols) == 0 {
		return nil, fmt.Errorf("query: nenhum campo válido fornecido para INSERT")
	}

	sql := fmt.Sprintf(
		"INSERT INTO %s.%s (%s) VALUES (%s) RETURNING *",
		schemaName, tableName,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	return &WriteQuery{SQL: sql, Args: args}, nil
}

// Campos desconhecidos retornam erro.
func BuildUpdate(schemaName, tableName string, table *registry.Table, id string, body map[string]any, ownerID string) (*WriteQuery, error) {
	known := columnSet(table)
	types := columnTypes(table)

	for key := range body {
		if _, skip := systemFields[key]; skip {
			continue
		}
		if _, ok := known[key]; !ok {
			return nil, fmt.Errorf("query: campo desconhecido no body: %q", key)
		}
	}

	// Builds SET clauses in deterministic column order, excluding system fields
	var setClauses []string
	var args []any

	for _, col := range table.Columns {
		if _, skip := systemFields[col.Name]; skip {
			continue
		}
		val, present := body[col.Name]
		if !present {
			continue
		}
		args = append(args, val)
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d%s", col.Name, len(args), pgCast(types[col.Name])))
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("query: nenhum campo válido fornecido para UPDATE")
	}

	setClauses = append(setClauses, "updated_at = now()")

	args = append(args, id)
	idPlaceholder := fmt.Sprintf("$%d", len(args))

	whereClause := "id = " + idPlaceholder
	if ownerID != "" {
		args = append(args, ownerID)
		whereClause += fmt.Sprintf(" AND owner_id = $%d::uuid", len(args))
	}
	sql := fmt.Sprintf(
		"UPDATE %s.%s SET %s WHERE %s RETURNING *",
		schemaName, tableName,
		strings.Join(setClauses, ", "),
		whereClause,
	)

	return &WriteQuery{SQL: sql, Args: args}, nil
}

// id and ownerID are included in Args; the caller uses pool.Query(ctx, q.SQL, q.Args...).
func BuildGetByID(schemaName, tableName string, id string, ownerID string) *WriteQuery {
	if ownerID != "" {
		return &WriteQuery{
			SQL:  fmt.Sprintf("SELECT * FROM %s.%s WHERE id = $1::uuid AND owner_id = $2::uuid", schemaName, tableName),
			Args: []any{id, ownerID},
		}
	}
	return &WriteQuery{
		SQL:  fmt.Sprintf("SELECT * FROM %s.%s WHERE id = $1::uuid", schemaName, tableName),
		Args: []any{id},
	}
}

// id and ownerID are included in Args; the caller uses pool.Exec(ctx, q.SQL, q.Args...).
// When softDelete is true, performs UPDATE SET deleted_at = now() instead of DELETE.
func BuildDelete(schemaName, tableName string, id string, ownerID string, softDelete bool) *WriteQuery {
	if softDelete {
		if ownerID != "" {
			return &WriteQuery{
				SQL:  fmt.Sprintf("UPDATE %s.%s SET deleted_at = now() WHERE id = $1::uuid AND owner_id = $2::uuid AND deleted_at IS NULL", schemaName, tableName),
				Args: []any{id, ownerID},
			}
		}
		return &WriteQuery{
			SQL:  fmt.Sprintf("UPDATE %s.%s SET deleted_at = now() WHERE id = $1::uuid AND deleted_at IS NULL", schemaName, tableName),
			Args: []any{id},
		}
	}
	if ownerID != "" {
		return &WriteQuery{
			SQL:  fmt.Sprintf("DELETE FROM %s.%s WHERE id = $1::uuid AND owner_id = $2::uuid", schemaName, tableName),
			Args: []any{id, ownerID},
		}
	}
	return &WriteQuery{
		SQL:  fmt.Sprintf("DELETE FROM %s.%s WHERE id = $1::uuid", schemaName, tableName),
		Args: []any{id},
	}
}
