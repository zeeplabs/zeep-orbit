package query

import (
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/registry"
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
	q, err := BuildList("app_billing", "invoices", tbl, map[string]string{}, "", false)
	if err != nil {
		t.Fatalf("esperava nil error, got: %v", err)
	}

	if !strings.Contains(q.SQL, "SELECT * FROM app_billing.invoices") {
		t.Errorf("SQL inesperado: %q", q.SQL)
	}

	if len(q.Args) != 2 {
		t.Fatalf("esperava 2 args (limit, offset), got %d", len(q.Args))
	}
	if q.Args[0] != 50 {
		t.Errorf("limit esperado 50, got %v", q.Args[0])
	}
	if q.Args[1] != 0 {
		t.Errorf("offset esperado 0, got %v", q.Args[1])
	}

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
		"status": "eq.paid",
		"order":  "amount.desc",
		"limit":  "10",
		"offset": "5",
	}
	q, err := BuildList("app_billing", "invoices", tbl, params, "", false)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if !strings.Contains(q.SQL, "WHERE") {
		t.Errorf("SQL deveria conter WHERE: %q", q.SQL)
	}
	if !strings.Contains(q.SQL, "ORDER BY amount DESC") {
		t.Errorf("SQL deveria conter ORDER BY amount DESC: %q", q.SQL)
	}

	if len(q.Args) != 3 {
		t.Fatalf("esperava 3 args, got %d: %v", len(q.Args), q.Args)
	}
	if q.Args[1] != 10 {
		t.Errorf("limit esperado 10, got %v", q.Args[1])
	}
	if q.Args[2] != 5 {
		t.Errorf("offset esperado 5, got %v", q.Args[2])
	}

	if !strings.Contains(q.CountSQL, "WHERE") {
		t.Errorf("CountSQL deveria conter WHERE: %q", q.CountSQL)
	}
	if strings.Contains(q.CountSQL, "LIMIT") {
		t.Errorf("CountSQL não deve conter LIMIT: %q", q.CountSQL)
	}
}

func TestBuildList_FilterOperators(t *testing.T) {
	tbl := testTable()

	tests := []struct {
		name     string
		params   map[string]string
		wantSQL  string
		wantArgs int
	}{
		{
			name:     "eq",
			params:   map[string]string{"status": "eq.paid"},
			wantSQL:  `status = $1`,
			wantArgs: 3,
		},
		{
			name:     "ne",
			params:   map[string]string{"status": "ne.cancelled"},
			wantSQL:  `status != $1`,
			wantArgs: 3,
		},
		{
			name:     "gt",
			params:   map[string]string{"amount": "gt.100"},
			wantSQL:  `amount > $1`,
			wantArgs: 3,
		},
		{
			name:     "gte",
			params:   map[string]string{"amount": "gte.100"},
			wantSQL:  `amount >= $1`,
			wantArgs: 3,
		},
		{
			name:     "lt",
			params:   map[string]string{"amount": "lt.50"},
			wantSQL:  `amount < $1`,
			wantArgs: 3,
		},
		{
			name:     "lte",
			params:   map[string]string{"amount": "lte.50"},
			wantSQL:  `amount <= $1`,
			wantArgs: 3,
		},
		{
			name:     "like",
			params:   map[string]string{"status": "like.%paid%"},
			wantSQL:  `status LIKE $1`,
			wantArgs: 3,
		},
		{
			name:     "ilike",
			params:   map[string]string{"status": "ilike.%Paid%"},
			wantSQL:  `status ILIKE $1`,
			wantArgs: 3,
		},
		{
			name:     "in",
			params:   map[string]string{"status": "in.paid,pending"},
			wantSQL:  `status IN ($1, $2)`,
			wantArgs: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := BuildList("app_billing", "invoices", tbl, tt.params, "", false)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if !strings.Contains(q.SQL, tt.wantSQL) {
				t.Errorf("SQL deveria conter %q, got: %q", tt.wantSQL, q.SQL)
			}
			if len(q.Args) != tt.wantArgs {
				t.Errorf("esperava %d args, got %d: %v", tt.wantArgs, len(q.Args), q.Args)
			}
		})
	}
}

func TestBuildList_UnknownOperator(t *testing.T) {
	tbl := testTable()
	params := map[string]string{
		"status": "invalid.paid",
	}
	_, err := BuildList("app_billing", "invoices", tbl, params, "", false)
	if err == nil {
		t.Fatal("esperava erro para operador inválido, got nil")
	}
}

func TestBuildList_UnknownField(t *testing.T) {
	tbl := testTable()
	params := map[string]string{
		"nonexistent": "eq.foo",
	}
	_, err := BuildList("app_billing", "invoices", tbl, params, "", false)
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
	_, err := BuildList("app_billing", "invoices", tbl, params, "", false)
	if err == nil {
		t.Fatal("esperava erro para campo desconhecido em order, got nil")
	}
}

func TestBuildList_LimitClamp(t *testing.T) {
	tbl := testTable()
	params := map[string]string{
		"limit": "9999",
	}
	q, err := BuildList("app_billing", "invoices", tbl, params, "", false)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
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
	q, err := BuildInsert("app_billing", "invoices", tbl, body, "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.HasPrefix(q.SQL, "INSERT INTO app_billing.invoices") {
		t.Errorf("SQL inesperado: %q", q.SQL)
	}
	if !strings.Contains(q.SQL, "RETURNING *") {
		t.Errorf("SQL deveria conter RETURNING *: %q", q.SQL)
	}
	if len(q.Args) != 3 {
		t.Errorf("esperava 3 args, got %d: %v", len(q.Args), q.Args)
	}
}

func TestBuildInsert_Required(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"amount": "50.00",
	}
	_, err := BuildInsert("app_billing", "invoices", tbl, body, "")
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
		"amount":      nil,
		"customer_id": "uuid-xyz",
	}
	_, err := BuildInsert("app_billing", "invoices", tbl, body, "")
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
	_, err := BuildInsert("app_billing", "invoices", tbl, body, "")
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
	q, err := BuildInsert("app_billing", "invoices", tbl, body, "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	colStart := strings.Index(q.SQL, "(")
	colEnd := strings.Index(q.SQL, ")")
	if colStart == -1 || colEnd == -1 {
		t.Fatalf("não encontrou lista de colunas no SQL: %q", q.SQL)
	}
	colList := q.SQL[colStart+1 : colEnd]
	for _, sys := range []string{"id", "created_at", "updated_at"} {
		for _, col := range strings.Split(colList, ", ") {
			if strings.TrimSpace(col) == sys {
				t.Errorf("system field %q não deveria aparecer nas colunas do INSERT: %q", sys, colList)
			}
		}
	}
	for _, arg := range q.Args {
		if arg == "should-be-ignored" || arg == "2024-01-01" {
			t.Errorf("valor de system field encontrado nos args: %v", arg)
		}
	}
}

// timestamptzTable is a dedicated fixture for the "" -> NULL normalization
// and timestamptz-format validation tests (INSERTERR-01, INSERTERR-06..08):
// closed_at is nullable, opened_at is required - testTable()'s created_at/
// updated_at are systemFields and always stripped by BuildInsert, so they
// can't exercise this behavior.
func timestamptzTable() *registry.Table {
	return &registry.Table{
		Name: "events",
		Columns: []registry.Column{
			{Name: "label", Type: "text", Required: true},
			{Name: "opened_at", Type: "timestamptz", Required: true},
			{Name: "closed_at", Type: "timestamptz", Required: false},
		},
	}
}

func TestBuildInsert_EmptyStringNormalizesToNullForNullableTimestamptz(t *testing.T) {
	tbl := timestamptzTable()
	body := map[string]any{
		"label":     "x",
		"opened_at": "2026-01-01T00:00:00Z",
		"closed_at": "",
	}
	q, err := BuildInsert("app_events", "events", tbl, body, "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	found := false
	for i, col := range []string{"label", "opened_at", "closed_at"} {
		if col == "closed_at" {
			found = true
			if q.Args[i] != nil {
				t.Errorf("esperava nil para closed_at, got %v (%T)", q.Args[i], q.Args[i])
			}
		}
	}
	if !found {
		t.Fatal("closed_at não apareceu nos args - teste mal formado")
	}
}

func TestBuildInsert_EmptyStringOnTextColumnUnchanged(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"amount":      "10.00",
		"customer_id": "uuid-abc",
		"status":      "",
	}
	q, err := BuildInsert("app_billing", "invoices", tbl, body, "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	statusFound := false
	for _, arg := range q.Args {
		if arg == "" {
			statusFound = true
		}
	}
	if !statusFound {
		t.Error("esperava \"\" preservada para coluna text (normalização não deve vazar pra outros tipos)")
	}
}

func TestBuildInsert_InvalidTimestampNamesColumn(t *testing.T) {
	tbl := timestamptzTable()
	body := map[string]any{
		"label":     "x",
		"opened_at": "not-a-timestamp",
	}
	_, err := BuildInsert("app_events", "events", tbl, body, "")
	if err == nil {
		t.Fatal("esperava erro para timestamptz inválido, got nil")
	}
	if !strings.Contains(err.Error(), "opened_at") {
		t.Errorf("mensagem de erro deveria citar 'opened_at': %v", err)
	}
	if strings.Contains(err.Error(), "not-a-timestamp") {
		t.Errorf("mensagem de erro não deve ecoar o valor tentado: %v", err)
	}
}

func TestBuildInsert_EmptyStringOnRequiredTimestamptzStillFails(t *testing.T) {
	tbl := timestamptzTable()
	body := map[string]any{
		"label":     "x",
		"opened_at": "",
	}
	_, err := BuildInsert("app_events", "events", tbl, body, "")
	if err == nil {
		t.Fatal("esperava erro para \"\" em coluna timestamptz required, got nil")
	}
	if !strings.Contains(err.Error(), "opened_at") {
		t.Errorf("mensagem de erro deveria citar 'opened_at': %v", err)
	}
}

func TestBuildInsert_ValidTimestampPassesThrough(t *testing.T) {
	tbl := timestamptzTable()
	body := map[string]any{
		"label":     "x",
		"opened_at": "2026-01-01T00:00:00Z",
	}
	q, err := BuildInsert("app_events", "events", tbl, body, "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	found := false
	for _, arg := range q.Args {
		if arg == "2026-01-01T00:00:00Z" {
			found = true
		}
	}
	if !found {
		t.Error("esperava valor de opened_at preservado sem alteração")
	}
}

// TestBuildInsert_OffsetlessTimestampFormsAccepted proves formats Postgres
// itself always accepted raw (date-only, and space/"T"-separated timestamps
// with no explicit UTC offset — interpreted in the session's TimeZone) still
// pass once Go-side validation was added. Pre-release review found these had
// silently regressed from "accepted" to a 400: neither time.RFC3339Nano nor
// pgtype.Timestamptz.Scan recognizes any of them, so without
// timestamptzOffsetlessLayouts every one of these would previously reject.
func TestBuildInsert_OffsetlessTimestampFormsAccepted(t *testing.T) {
	tbl := timestamptzTable()
	cases := []string{
		"2026-01-01",
		"2026-01-01 00:00:00",
		"2026-01-01T00:00:00",
	}
	for _, val := range cases {
		body := map[string]any{"label": "x", "opened_at": val}
		if _, err := BuildInsert("app_events", "events", tbl, body, ""); err != nil {
			t.Errorf("valor %q deveria ser aceito (Postgres aceita raw), obtido erro: %v", val, err)
		}
	}
}

// conflictTable is a fixture with a unique-ish column for ON CONFLICT tests.
func conflictTable() *registry.Table {
	return &registry.Table{
		Name: "events",
		Columns: []registry.Column{
			{Name: "label", Type: "text", Required: true},
			{Name: "external_id", Type: "text", Required: false, Unique: true},
		},
	}
}

func TestBuildInsert_OnConflictIgnoreNoTargetBareDoNothing(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x"}
	q, err := BuildInsertWithOptions("app_events", "events", tbl, body, "", InsertOptions{OnConflict: "ignore"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.Contains(q.SQL, "ON CONFLICT DO NOTHING") {
		t.Errorf("SQL deveria conter ON CONFLICT DO NOTHING sem alvo: %q", q.SQL)
	}
	if strings.Contains(q.SQL, "ON CONFLICT (") {
		t.Errorf("SQL não deveria ter alvo de conflito quando conflict_columns está vazio: %q", q.SQL)
	}
}

func TestBuildInsert_OnConflictIgnoreWithTarget(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x"}
	q, err := BuildInsertWithOptions("app_events", "events", tbl, body, "", InsertOptions{OnConflict: "ignore", ConflictColumns: []string{"external_id"}})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.Contains(q.SQL, "ON CONFLICT (external_id) DO NOTHING") {
		t.Errorf("SQL deveria conter ON CONFLICT (external_id) DO NOTHING: %q", q.SQL)
	}
}

func TestBuildInsert_OnConflictUpdateSetsNonTargetColumns(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x", "external_id": "ext-1"}
	q, err := BuildInsertWithOptions("app_events", "events", tbl, body, "", InsertOptions{OnConflict: "update", ConflictColumns: []string{"external_id"}})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.Contains(q.SQL, "ON CONFLICT (external_id) DO UPDATE SET label = excluded.label, updated_at = now()") {
		t.Errorf("SQL de update inesperado: %q", q.SQL)
	}
	if strings.Contains(q.SQL, "external_id = excluded.external_id") {
		t.Errorf("SET não deveria incluir a própria coluna de conflito: %q", q.SQL)
	}
}

// TestBuildInsert_OnConflictUpdateExcludesOwnerID proves owner_id is never
// part of the ON CONFLICT DO UPDATE SET clause, even when ownerID is set —
// upserting a conflicting row must not reassign its ownership. Uses a
// non-empty ownerID specifically: every other on_conflict:"update" test
// uses "" (no owner_id column at all), which can't distinguish "owner_id is
// correctly excluded" from "owner_id is absent and therefore trivially not
// in the SET list".
func TestBuildInsert_OnConflictUpdateExcludesOwnerID(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x", "external_id": "ext-1"}
	q, err := BuildInsertWithOptions("app_events", "events", tbl, body, "owner-uuid-123", InsertOptions{OnConflict: "update", ConflictColumns: []string{"external_id"}})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if strings.Contains(q.SQL, "owner_id = excluded.owner_id") {
		t.Errorf("SET não deveria reatribuir owner_id no upsert: %q", q.SQL)
	}
	if !strings.Contains(q.SQL, "owner_id") {
		t.Errorf("owner_id deveria continuar na lista de colunas do INSERT (só não no SET): %q", q.SQL)
	}
}

// TestBuildInsert_OnConflictUpdateOwnerFilterAddsWhereGuard proves
// ConflictOwnerFilter adds a WHERE owner_id = $n guard to the DO UPDATE —
// second pre-release review finding: excluding owner_id from the SET clause
// (proven above) only stops an upsert from reassigning ownership, it
// doesn't stop the upsert from overwriting a row belonging to a different
// owner. The guard is what actually closes that gap.
func TestBuildInsert_OnConflictUpdateOwnerFilterAddsWhereGuard(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x", "external_id": "ext-1"}
	q, err := BuildInsertWithOptions("app_events", "events", tbl, body, "owner-uuid-123", InsertOptions{
		OnConflict: "update", ConflictColumns: []string{"external_id"}, ConflictOwnerFilter: "owner-uuid-123",
	})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.Contains(q.SQL, "WHERE events.owner_id = $") {
		t.Errorf("SQL deveria conter guarda WHERE por owner_id: %q", q.SQL)
	}
	if q.Args[len(q.Args)-1] != "owner-uuid-123" {
		t.Errorf("último arg deveria ser o owner filter, obtido %v", q.Args[len(q.Args)-1])
	}
}

// TestBuildInsert_OnConflictUpdateNoOwnerFilterNoWhereGuard proves the guard
// is omitted entirely for "policy" RLS mode (ConflictOwnerFilter == "",
// since filterOwner never auto-scopes that mode) — native Postgres policies
// are the enforcement there, not an app-level WHERE.
func TestBuildInsert_OnConflictUpdateNoOwnerFilterNoWhereGuard(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x", "external_id": "ext-1"}
	q, err := BuildInsertWithOptions("app_events", "events", tbl, body, "", InsertOptions{
		OnConflict: "update", ConflictColumns: []string{"external_id"},
	})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if strings.Contains(q.SQL, "WHERE") {
		t.Errorf("SQL não deveria conter guarda WHERE sem ConflictOwnerFilter: %q", q.SQL)
	}
}

// TestBuildInsert_OnConflictUpdateReturningIncludesInsertMarker proves the
// RETURNING clause for on_conflict:"update" carries the xmax-based
// zeep_was_insert marker the handler uses to answer 201 vs 200 correctly —
// without it, a fresh row (no real conflict) would misreport as 200
// ("updated") to any client keying off status code.
func TestBuildInsert_OnConflictUpdateReturningIncludesInsertMarker(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x", "external_id": "ext-1"}
	q, err := BuildInsertWithOptions("app_events", "events", tbl, body, "", InsertOptions{OnConflict: "update", ConflictColumns: []string{"external_id"}})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !strings.Contains(q.SQL, "RETURNING *, (xmax = 0) AS zeep_was_insert") {
		t.Errorf("SQL deveria conter o marcador zeep_was_insert: %q", q.SQL)
	}
}

// TestBuildInsert_OnConflictIgnoreReturningHasNoInsertMarker proves the
// marker is scoped only to "update" — "ignore" and plain inserts don't need
// it and shouldn't carry the extra column into every response.
func TestBuildInsert_OnConflictIgnoreReturningHasNoInsertMarker(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x", "external_id": "ext-1"}
	q, err := BuildInsertWithOptions("app_events", "events", tbl, body, "", InsertOptions{OnConflict: "ignore"})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if strings.Contains(q.SQL, "zeep_was_insert") {
		t.Errorf("on_conflict:ignore não deveria carregar zeep_was_insert: %q", q.SQL)
	}
}

func TestBuildInsert_OnConflictAbsentUnchanged(t *testing.T) {
	tbl := conflictTable()
	body := map[string]any{"label": "x"}
	withOpts, err := BuildInsertWithOptions("app_events", "events", tbl, body, "", InsertOptions{})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	plain, err := BuildInsert("app_events", "events", tbl, body, "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if withOpts.SQL != plain.SQL {
		t.Errorf("SQL deveria ser idêntico sem on_conflict: %q vs %q", withOpts.SQL, plain.SQL)
	}
	if strings.Contains(plain.SQL, "ON CONFLICT") {
		t.Errorf("SQL sem on_conflict não deveria conter ON CONFLICT: %q", plain.SQL)
	}
}

// ── BuildUpdate ───────────────────────────────────────────────────────────────

func TestBuildUpdate_Valid(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"status": "paid",
	}
	q, err := BuildUpdate("app_billing", "invoices", tbl, "uuid-999", body, "")
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
	if len(q.Args) != 2 {
		t.Fatalf("esperava 2 args, got %d: %v", len(q.Args), q.Args)
	}
	last := q.Args[len(q.Args)-1]
	if last != "uuid-999" {
		t.Errorf("último arg deveria ser o id 'uuid-999', got %v", last)
	}
}

// TestBuildUpdate_EmptyStringNormalizesToNullForNullableTimestamptz proves
// BuildUpdate applies the same "" -> NULL timestamptz normalization as
// BuildInsert (asymmetry found in pre-release review: PATCH with "" on a
// nullable timestamptz previously reached Postgres raw and 500'd).
func TestBuildUpdate_EmptyStringNormalizesToNullForNullableTimestamptz(t *testing.T) {
	tbl := timestamptzTable()
	body := map[string]any{"closed_at": ""}
	q, err := BuildUpdate("app_events", "events", tbl, "uuid-1", body, "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if q.Args[0] != nil {
		t.Errorf("esperava nil para closed_at, got %v (%T)", q.Args[0], q.Args[0])
	}
}

// TestBuildUpdate_InvalidTimestampNamesColumn mirrors
// TestBuildInsert_InvalidTimestampNamesColumn for the UPDATE path.
func TestBuildUpdate_InvalidTimestampNamesColumn(t *testing.T) {
	tbl := timestamptzTable()
	body := map[string]any{"opened_at": "not-a-timestamp"}
	_, err := BuildUpdate("app_events", "events", tbl, "uuid-1", body, "")
	if err == nil {
		t.Fatal("esperava erro para timestamptz inválido, got nil")
	}
	if !strings.Contains(err.Error(), "opened_at") {
		t.Errorf("mensagem de erro deveria citar 'opened_at': %v", err)
	}
}

func TestBuildUpdate_UnknownField(t *testing.T) {
	tbl := testTable()
	body := map[string]any{
		"status":   "paid",
		"evil_col": "1=1",
	}
	_, err := BuildUpdate("app_billing", "invoices", tbl, "uuid-1", body, "")
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
	q, err := BuildUpdate("app_billing", "invoices", tbl, "uuid-2", body, "")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if strings.Contains(q.SQL, "id =") && !strings.Contains(q.SQL, "WHERE id =") {
		t.Errorf("'id' não deveria aparecer no SET: %q", q.SQL)
	}
	if strings.Contains(q.SQL, "created_at =") {
		t.Errorf("'created_at' não deveria aparecer no SET: %q", q.SQL)
	}
}

// ── BuildGetByID ──────────────────────────────────────────────────────────────

func TestBuildGetByID(t *testing.T) {
	q := BuildGetByID("app_billing", "invoices", "uuid-test", "")
	expected := "SELECT * FROM app_billing.invoices WHERE id = $1::uuid"
	if q.SQL != expected {
		t.Errorf("SQL esperado %q, got %q", expected, q.SQL)
	}
	if len(q.Args) != 1 || q.Args[0] != "uuid-test" {
		t.Errorf("Args deveria ser [uuid-test], got %v", q.Args)
	}
}

// ── BuildDelete ───────────────────────────────────────────────────────────────

func TestBuildDelete(t *testing.T) {
	q := BuildDelete("app_billing", "invoices", "uuid-del", "", false)
	expected := "DELETE FROM app_billing.invoices WHERE id = $1::uuid"
	if q.SQL != expected {
		t.Errorf("SQL esperado %q, got %q", expected, q.SQL)
	}
	if len(q.Args) != 1 || q.Args[0] != "uuid-del" {
		t.Errorf("Args deveria ser [uuid-del], got %v", q.Args)
	}
}
