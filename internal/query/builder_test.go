package query

import (
	"strings"
	"testing"

	"github.com/zeep-tecnologia/zeep-core/internal/registry"
)

// testTable retorna uma Table de teste com colunas representativas.
func testTable() *registry.Table {
	return &registry.Table{
		Name: "invoices",
		Columns: []registry.Column{
			{Name: "id", Type: "uuid"},
			{Name: "created_at", Type: "timestamptz"},
			{Name: "updated_at", Type: "timestamptz"},
			{Name: "amount", Type: "numeric", Required: true},
			{Name: "status", Type: "text"},
			{Name: "customer_id", Type: "uuid", Required: true},
		},
	}
}

// ── BuildList ─────────────────────────────────────────────────────────────────

func TestBuildList_Basic(t *testing.T) {
	tbl := testTable()
	q, err := BuildList("app_billing", "invoices", tbl, map[string]string{})
	if err != nil {
		t.Fatalf("esperava nil error, got: %v", err)
	}

	// SQL deve conter SELECT * e LIMIT/OFFSET
	if !strings.Contains(q.SQL, "SELECT * FROM app_billing.invoices") {
		t.Errorf("SQL inesperado: %q", q.SQL)
	}

	// Últimos dois args devem ser limit=50 e offset=0
	if len(q.Args) != 2 {
		t.Fatalf("esperava 2 args (limit, offset), got %d", len(q.Args))
	}
	if q.Args[0] != 50 {
		t.Errorf("limit esperado 50, got %v", q.Args[0])
	}
	if q.Args[1] != 0 {
		t.Errorf("offset esperado 0, got %v", q.Args[1])
	}

	// CountSQL não deve ter LIMIT/OFFSET
	if strings.Contains(q.CountSQL, "LIMIT") || strings.Contains(q.CountSQL, "OFFSET") {
		t.Errorf("CountSQL não deve conter LIMIT/OFFSET: %q", q.CountSQL)
	}
	if !strings.HasPrefix(q.CountSQL, "SELECT COUNT(*)") {
		t.Errorf("CountSQL deve começar com SELECT COUNT(*): %q", q.CountSQL)
	}
}

func TestBuildList_WithFilters(t *testing.T) {
	tbl := testTable()
	params := map[string]string{
		"status":  "eq.paid",
		"order":   "amount.desc",
		"limit":   "10",
		"offset":  "5",
	}
	q, err := BuildList("app_billing", "invoices", tbl, params)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if !strings.Contains(q.SQL, "WHERE") {
		t.Errorf("SQL deveria conter WHERE: %q", q.SQL)
	}
	if !strings.Contains(q.SQL, "ORDER BY amount DESC") {
		t.Errorf("SQL deveria conter ORDER BY amount DESC: %q", q.SQL)
	}

	// Args: 1 filtro + limit + offset = 3
	if len(q.Args) != 3 {
		t.Fatalf("esperava 3 args, got %d: %v", len(q.Args), q.Args)
	}
	if q.Args[1] != 10 {
		t.Errorf("limit esperado 10, got %v", q.Args[1])
	}
	if q.Args[2] != 5 {
		t.Errorf("offset esperado 5, got %v", q.Args[2])
	}

	// CountSQL deve ter o filtro mas não LIMIT/OFFSET
	if !strings.Contains(q.CountSQL, "WHERE") {
		t.Errorf("CountSQL deveria conter WHERE: %q", q.CountSQL)
	}
	if strings.Contains(q.CountSQL, "LIMIT") {
		t.Errorf("CountSQL não deve conter LIMIT: %q", q.CountSQL)
	}
}

func TestBuildList_UnknownField(t *testing.T) {
	tbl := testTable()
	params := map[string]string{
		"nonexistent": "eq.foo",
	}
	_, err := BuildList("app_billing", "invoices", tbl, params)
	if err == nil {
		t.Fatal("esperava erro para campo desconhecido, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("mensagem de erro deveria mencionar o campo: %v", err)
	}
}

func TestBuildList_UnknownFieldInOrder(t *testing.T) {
	tbl := testTable()
	params := map[string]string{
		"order": "nonexistent.asc",
	}
	_, err := BuildList("app_billing", "invoices", tbl, params)
	if err == nil {
		t.Fatal("esperava erro para campo desconhecido em order, got nil")
	}
}

func TestBuildList_LimitClamp(t *testing.T) {
	tbl := testTable()
	params := map[string]string{
		"limit": "9999",
	}
	q, err := BuildList("app_billing", "invoices", tbl, params)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// Limit deve ter sido clampado para 1000
	if q.Args[0] != 1000 {
		t.Errorf("limit esperado 1000 (clamp), got %v", q.Args[0])
	}
}

// ── BuildInsert ───────────────────────────────────────────────────────────────

func TestBuildInsert_Valid(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"amount":      "100.00",
		"customer_id": "uuid-abc",
		"status":      "pending",
	}
	q, err := BuildInsert("app_billing", "invoices", tbl, body)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.HasPrefix(q.SQL, "INSERT INTO app_billing.invoices") {
		t.Errorf("SQL inesperado: %q", q.SQL)
	}
	if !strings.Contains(q.SQL, "RETURNING *") {
		t.Errorf("SQL deveria conter RETURNING *: %q", q.SQL)
	}
	// Deve ter 3 args (amount, status, customer_id — em ordem das colunas da tabela)
	if len(q.Args) != 3 {
		t.Errorf("esperava 3 args, got %d: %v", len(q.Args), q.Args)
	}
}

func TestBuildInsert_Required(t *testing.T) {
	tbl := testTable()
	// Falta customer_id que é required
	body := map[string]any{
		"amount": "50.00",
	}
	_, err := BuildInsert("app_billing", "invoices", tbl, body)
	if err == nil {
		t.Fatal("esperava erro por campo required ausente, got nil")
	}
	if !strings.Contains(err.Error(), "customer_id") {
		t.Errorf("mensagem de erro deveria mencionar 'customer_id': %v", err)
	}
}

func TestBuildInsert_RequiredNull(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"amount":      nil, // presente mas null
		"customer_id": "uuid-xyz",
	}
	_, err := BuildInsert("app_billing", "invoices", tbl, body)
	if err == nil {
		t.Fatal("esperava erro por campo required null, got nil")
	}
	if !strings.Contains(err.Error(), "amount") {
		t.Errorf("mensagem de erro deveria mencionar 'amount': %v", err)
	}
}

func TestBuildInsert_UnknownField(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"amount":      "10.00",
		"customer_id": "uuid-abc",
		"hack_field":  "DROP TABLE",
	}
	_, err := BuildInsert("app_billing", "invoices", tbl, body)
	if err == nil {
		t.Fatal("esperava erro para campo desconhecido, got nil")
	}
	if !strings.Contains(err.Error(), "hack_field") {
		t.Errorf("mensagem de erro deveria mencionar 'hack_field': %v", err)
	}
}

func TestBuildInsert_StripsSystemFields(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"id":          "should-be-ignored",
		"created_at":  "2024-01-01",
		"updated_at":  "2024-01-01",
		"amount":      "42.00",
		"customer_id": "uuid-123",
	}
	q, err := BuildInsert("app_billing", "invoices", tbl, body)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// id, created_at e updated_at não devem aparecer na lista de colunas do INSERT.
	// Extraímos a substring entre "(" e ")" da cláusula de colunas.
	colStart := strings.Index(q.SQL, "(")
	colEnd := strings.Index(q.SQL, ")")
	if colStart == -1 || colEnd == -1 {
		t.Fatalf("não encontrou lista de colunas no SQL: %q", q.SQL)
	}
	colList := q.SQL[colStart+1 : colEnd]
	for _, sys := range []string{"id", "created_at", "updated_at"} {
		// Verifica se o sistema field aparece como coluna completa (vírgula ou borda)
		for _, col := range strings.Split(colList, ", ") {
			if strings.TrimSpace(col) == sys {
				t.Errorf("system field %q não deveria aparecer nas colunas do INSERT: %q", sys, colList)
			}
		}
	}
	// Args não devem conter os valores de system fields
	for _, arg := range q.Args {
		if arg == "should-be-ignored" || arg == "2024-01-01" {
			t.Errorf("valor de system field encontrado nos args: %v", arg)
		}
	}
}

// ── BuildUpdate ───────────────────────────────────────────────────────────────

func TestBuildUpdate_Valid(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"status": "paid",
	}
	q, err := BuildUpdate("app_billing", "invoices", tbl, "uuid-999", body)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.HasPrefix(q.SQL, "UPDATE app_billing.invoices SET") {
		t.Errorf("SQL inesperado: %q", q.SQL)
	}
	if !strings.Contains(q.SQL, "updated_at = now()") {
		t.Errorf("SQL deveria conter updated_at = now(): %q", q.SQL)
	}
	if !strings.Contains(q.SQL, "WHERE id =") {
		t.Errorf("SQL deveria conter WHERE id =: %q", q.SQL)
	}
	if !strings.Contains(q.SQL, "RETURNING *") {
		t.Errorf("SQL deveria conter RETURNING *: %q", q.SQL)
	}
	// Args: status + id (updated_at não usa placeholder)
	if len(q.Args) != 2 {
		t.Fatalf("esperava 2 args, got %d: %v", len(q.Args), q.Args)
	}
	last := q.Args[len(q.Args)-1]
	if last != "uuid-999" {
		t.Errorf("último arg deveria ser o id 'uuid-999', got %v", last)
	}
}

func TestBuildUpdate_UnknownField(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"status":    "paid",
		"evil_col":  "1=1",
	}
	_, err := BuildUpdate("app_billing", "invoices", tbl, "uuid-1", body)
	if err == nil {
		t.Fatal("esperava erro para campo desconhecido, got nil")
	}
	if !strings.Contains(err.Error(), "evil_col") {
		t.Errorf("mensagem de erro deveria mencionar 'evil_col': %v", err)
	}
}

func TestBuildUpdate_SystemFieldsStripped(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"id":         "ignored",
		"created_at": "ignored",
		"status":     "refunded",
	}
	q, err := BuildUpdate("app_billing", "invoices", tbl, "uuid-2", body)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// id e created_at não devem aparecer como SET clause
	if strings.Contains(q.SQL, "id =") && !strings.Contains(q.SQL, "WHERE id =") {
		t.Errorf("'id' não deveria aparecer no SET: %q", q.SQL)
	}
	if strings.Contains(q.SQL, "created_at =") {
		t.Errorf("'created_at' não deveria aparecer no SET: %q", q.SQL)
	}
}

// ── BuildGetByID ──────────────────────────────────────────────────────────────

func TestBuildGetByID(t *testing.T) {
	q := BuildGetByID("app_billing", "invoices")
	expected := "SELECT * FROM app_billing.invoices WHERE id = $1"
	if q.SQL != expected {
		t.Errorf("SQL esperado %q, got %q", expected, q.SQL)
	}
	if q.Args != nil {
		t.Errorf("Args deveria ser nil, got %v", q.Args)
	}
}

// ── BuildDelete ───────────────────────────────────────────────────────────────

func TestBuildDelete(t *testing.T) {
	q := BuildDelete("app_billing", "invoices")
	expected := "DELETE FROM app_billing.invoices WHERE id = $1"
	if q.SQL != expected {
		t.Errorf("SQL esperado %q, got %q", expected, q.SQL)
	}
	if q.Args != nil {
		t.Errorf("Args deveria ser nil, got %v", q.Args)
	}
}
