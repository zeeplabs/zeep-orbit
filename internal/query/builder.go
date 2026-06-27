package query

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// ListQuery é o resultado de BuildList: SQL paginado + CountSQL sem paginação.
type ListQuery struct {
	SQL      string
	Args     []any
	CountSQL string // SELECT COUNT(*) ... (mesmos filtros, sem LIMIT/OFFSET)
}

// WriteQuery é o resultado de operações de escrita (INSERT, UPDATE, DELETE, GET by ID).
type WriteQuery struct {
	SQL  string
	Args []any
}

// columnSet retorna um conjunto (map) de nomes de colunas conhecidas para lookup O(1).
func columnSet(table *registry.Table) map[string]struct{} {
	set := make(map[string]struct{}, len(table.Columns))
	for _, col := range table.Columns {
		set[col.Name] = struct{}{}
	}
	return set
}

// columnTypes retorna map[colName]colType para lookup rápido de tipo.
func columnTypes(table *registry.Table) map[string]string {
	m := make(map[string]string, len(table.Columns))
	for _, col := range table.Columns {
		m[col.Name] = col.Type
	}
	return m
}

// pgCast retorna o cast SQL necessário para o tipo da coluna no extended protocol.
// uuid e timestamptz não têm auto-cast de text no protocolo estendido do pgx.
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

// systemFields são campos gerenciados pelo servidor que nunca devem vir do caller.
var systemFields = map[string]struct{}{
	"id":         {},
	"created_at": {},
	"updated_at": {},
	"owner_id":   {},
}

// BuildList constrói SELECT paginado para GET /{app}/{table}.
//
// Parâmetros aceitos em params:
//   - limit: inteiro positivo (default 50, max 1000)
//   - offset: inteiro não-negativo (default 0)
//   - {field}=eq.{value}: filtro de igualdade; campo deve existir na tabela
//   - order={field}.asc|{field}.desc: ordenação; campo deve existir na tabela
func BuildList(schemaName, tableName string, table *registry.Table, params map[string]string, ownerID string) (*ListQuery, error) {
	const defaultLimit = 50
	const maxLimit = 1000

	known := columnSet(table)
	types := columnTypes(table)

	// --- limit ---
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

	// --- offset ---
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

	for key, val := range params {
		if key == "limit" || key == "offset" || key == "order" {
			continue
		}
		// Formato esperado: {field}=eq.{value}  →  params["field"] = "eq.value"
		if !strings.HasPrefix(val, "eq.") {
			return nil, fmt.Errorf("query: operador não suportado em filtro '%s=%s' (use eq.)", key, val)
		}
		if _, ok := known[key]; !ok {
			return nil, fmt.Errorf("query: campo desconhecido no filtro: %q", key)
		}
		filterVal := strings.TrimPrefix(val, "eq.")
		args = append(args, filterVal)
		whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d%s", key, len(args), pgCast(types[key])))
	}

	if ownerID != "" {
		args = append(args, ownerID)
		whereClauses = append(whereClauses, fmt.Sprintf("owner_id = $%d::uuid", len(args)))
	}

	// --- order ---
	var orderClause string
	if raw, ok := params["order"]; ok {
		// Formato: {field}.asc ou {field}.desc
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

	// --- monta WHERE ---
	baseFrom := fmt.Sprintf("FROM %s.%s", schemaName, tableName)
	var whereStr string
	if len(whereClauses) > 0 {
		whereStr = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	// CountSQL (sem LIMIT/OFFSET)
	countSQL := fmt.Sprintf("SELECT COUNT(*) %s%s", baseFrom, whereStr)

	// SQL principal
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

// BuildInsert constrói INSERT para POST /{app}/{table}.
//
// Strip silencioso: id, created_at, updated_at são ignorados mesmo que venham no body.
// Campos required ausentes ou null retornam erro.
// Campos desconhecidos retornam erro.
func BuildInsert(schemaName, tableName string, table *registry.Table, body map[string]any, ownerID string) (*WriteQuery, error) {
	known := columnSet(table)
	types := columnTypes(table)

	// Valida campos do body
	for key := range body {
		if _, skip := systemFields[key]; skip {
			continue
		}
		if _, ok := known[key]; !ok {
			return nil, fmt.Errorf("query: campo desconhecido no body: %q", key)
		}
	}

	// Valida required
	for _, col := range table.Columns {
		if !col.Required {
			continue
		}
		val, present := body[col.Name]
		if !present || val == nil {
			return nil, fmt.Errorf("query: campo %q é obrigatório", col.Name)
		}
	}

	// Monta listas de colunas e placeholders na ordem das colunas da tabela (determinístico)
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

// BuildUpdate constrói UPDATE parcial para PATCH /{app}/{table}/{id}.
//
// Strip silencioso: id, created_at, updated_at são ignorados mesmo que venham no body.
// updated_at = now() é sempre incluído server-side.
// Campos desconhecidos retornam erro.
func BuildUpdate(schemaName, tableName string, table *registry.Table, id string, body map[string]any, ownerID string) (*WriteQuery, error) {
	known := columnSet(table)
	types := columnTypes(table)

	// Valida campos do body
	for key := range body {
		if _, skip := systemFields[key]; skip {
			continue
		}
		if _, ok := known[key]; !ok {
			return nil, fmt.Errorf("query: campo desconhecido no body: %q", key)
		}
	}

	// Monta SET clauses na ordem das colunas (determinístico), excluindo system fields
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

	// updated_at server-side (não usa placeholder — função SQL)
	setClauses = append(setClauses, "updated_at = now()")

	// id é o último argumento
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

// BuildGetByID constrói SELECT por id para GET /{app}/{table}/{id}.
// id e ownerID são incluídos em Args; o caller usa pool.Query(ctx, q.SQL, q.Args...).
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

// BuildDelete constrói DELETE por id para DELETE /{app}/{table}/{id}.
// id e ownerID são incluídos em Args; o caller usa pool.Exec(ctx, q.SQL, q.Args...).
func BuildDelete(schemaName, tableName string, id string, ownerID string) *WriteQuery {
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
