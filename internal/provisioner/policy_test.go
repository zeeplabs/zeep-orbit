package provisioner

import (
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

func testColumns() []config.ColumnConfig {
	return []config.ColumnConfig{
		{Name: "requester_id", Type: "uuid"},
		{Name: "approved_by", Type: "uuid"},
		{Name: "amount", Type: "decimal"},
		{Name: "created_at", Type: "timestamptz"},
		{Name: "status", Type: "text"},
	}
}

func TestBuildPolicySQL_ValidEqualityClaim(t *testing.T) {
	def := PolicyDef{
		Name:   "approver_no_self_approve",
		Action: "update",
		Roles:  []string{"approver"},
		Clauses: []PolicyClause{
			{Column: "requester_id", Operator: "!=", ValueSource: "claim", Value: "sub"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantUsing := `USING ((current_setting('app.jwt_role', true) = ANY (ARRAY['approver'])) AND ("requester_id" != current_setting('app.jwt_sub', true)::UUID))`
	if !strings.Contains(sql, wantUsing) {
		t.Fatalf("sql = %q, want it to contain %q", sql, wantUsing)
	}
	if !strings.Contains(sql, `WITH CHECK (`) {
		t.Fatalf("update policy must include WITH CHECK, got %q", sql)
	}
	if !strings.HasPrefix(sql, `CREATE POLICY "approver_no_self_approve" ON "app_schema"."requests" FOR UPDATE TO zeep_app_enduser`) {
		t.Fatalf("unexpected prefix: %q", sql)
	}
}

func TestBuildPolicySQL_SelectHasNoWithCheck(t *testing.T) {
	def := PolicyDef{
		Name:   "select_policy",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "active"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(sql, "WITH CHECK") {
		t.Fatalf("select policy must not include WITH CHECK, got %q", sql)
	}
}

func TestBuildPolicySQL_InsertHasWithCheck(t *testing.T) {
	def := PolicyDef{
		Name:   "insert_policy",
		Action: "insert",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "active"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "WITH CHECK") {
		t.Fatalf("insert policy must include WITH CHECK, got %q", sql)
	}
}

func TestBuildPolicySQL_RejectsUnknownColumn(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "does_not_exist", Operator: "=", ValueSource: "literal", Value: "x"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for unknown column, got nil")
	}
}

func TestBuildPolicySQL_RejectsColumnFailingIdentRe(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "Bad-Column;drop", Operator: "=", ValueSource: "literal", Value: "x"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for column failing identRe, got nil")
	}
}

func TestBuildPolicySQL_RejectsOperatorOutsideAllowlist(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "LIKE", ValueSource: "literal", Value: "x"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for disallowed operator, got nil")
	}
}

func TestBuildPolicySQL_RejectsInvalidClaim(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "requester_id", Operator: "=", ValueSource: "claim", Value: "admin_flag"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for claim outside role/sub/email, got nil")
	}
}

func TestBuildPolicySQL_AcceptsEachAllowedClaim(t *testing.T) {
	for _, claim := range []string{"role", "sub", "email"} {
		def := PolicyDef{
			Name:   "p_" + claim,
			Action: "select",
			Roles:  []string{"member"},
			Clauses: []PolicyClause{
				{Column: "status", Operator: "=", ValueSource: "claim", Value: claim},
			},
		}
		sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
		if err != nil {
			t.Fatalf("claim %q: unexpected error: %v", claim, err)
		}
		if !strings.Contains(sql, "current_setting('app.jwt_"+claim+"', true)") {
			t.Fatalf("claim %q: expected GUC reference in sql, got %q", claim, sql)
		}
	}
}

func TestBuildPolicySQL_LiteralInjectionIsEscapedSafely(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: `x'; DROP TABLE requests; --`},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The literal must be fully contained inside a single-quoted string —
	// the embedded quote must be doubled, never left as a lone terminator
	// that breaks out of the literal.
	want := `"status" = 'x''; DROP TABLE requests; --'::TEXT`
	if !strings.Contains(sql, want) {
		t.Fatalf("sql = %q, want it to contain safely-escaped %q", sql, want)
	}
	// Sanity: exactly the escaped literal appears, not the raw unescaped value.
	if strings.Contains(sql, `'x';`) {
		t.Fatalf("literal escaped incorrectly, raw quote leaked: %q", sql)
	}
}

func TestBuildPolicySQL_LiteralWithBackslashUsesEscapeStringSyntax(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: `a\b`},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `"status" = E'a\\b'::TEXT`
	if !strings.Contains(sql, want) {
		t.Fatalf("sql = %q, want it to contain %q", sql, want)
	}
}

func TestBuildPolicySQL_NumericComparisonOperators(t *testing.T) {
	cases := []struct {
		op          string
		valueSource string
		value       string
		want        string
	}{
		{">", "literal", "100", `"amount" > '100'::DECIMAL`},
		{"<", "literal", "100", `"amount" < '100'::DECIMAL`},
		{">=", "literal", "100", `"amount" >= '100'::DECIMAL`},
		{"<=", "literal", "100", `"amount" <= '100'::DECIMAL`},
		{">", "claim", "sub", `"amount" > current_setting('app.jwt_sub', true)::DECIMAL`},
	}
	for _, c := range cases {
		def := PolicyDef{
			Name:   "p",
			Action: "select",
			Roles:  []string{"member"},
			Clauses: []PolicyClause{
				{Column: "amount", Operator: c.op, ValueSource: c.valueSource, Value: c.value},
			},
		}
		sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
		if err != nil {
			t.Fatalf("op %q: unexpected error: %v", c.op, err)
		}
		if !strings.Contains(sql, c.want) {
			t.Fatalf("op %q: sql = %q, want it to contain %q", c.op, sql, c.want)
		}
	}
}

func TestBuildPolicySQL_DateComparisonOperator(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "created_at", Operator: "<=", ValueSource: "literal", Value: "2026-01-01T00:00:00Z"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `"created_at" <= '2026-01-01T00:00:00Z'::TIMESTAMPTZ`
	if !strings.Contains(sql, want) {
		t.Fatalf("sql = %q, want it to contain %q", sql, want)
	}
}

func TestBuildPolicySQL_IsNullProducesUnarySQL(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "approved_by", Operator: "IS NULL"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `"approved_by" IS NULL`
	if !strings.Contains(sql, want) {
		t.Fatalf("sql = %q, want it to contain %q", sql, want)
	}
}

func TestBuildPolicySQL_IsNotNullProducesUnarySQL(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "approved_by", Operator: "IS NOT NULL"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `"approved_by" IS NOT NULL`
	if !strings.Contains(sql, want) {
		t.Fatalf("sql = %q, want it to contain %q", sql, want)
	}
}

func TestBuildPolicySQL_UnaryOperatorRejectsValue(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "approved_by", Operator: "IS NULL", ValueSource: "literal", Value: "x"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for IS NULL with a value, got nil")
	}
}

func TestBuildPolicySQL_INOperator(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IN", ValueSource: "literal", Value: "active, pending"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `"status" IN ('active'::TEXT, 'pending'::TEXT)`
	if !strings.Contains(sql, want) {
		t.Fatalf("sql = %q, want it to contain %q", sql, want)
	}
}

func TestBuildPolicySQL_NotINOperator(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "NOT IN", ValueSource: "literal", Value: "closed"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `"status" NOT IN ('closed'::TEXT)`
	if !strings.Contains(sql, want) {
		t.Fatalf("sql = %q, want it to contain %q", sql, want)
	}
}

// TestBuildPolicySQL_ThreeClauseFoldExactParenthesization is the load-bearing
// test for spec ROWPOL-29: it asserts the exact string produced by folding
// three clauses left-to-right (logic on c2="AND", c3="OR" must fold to
// "((c1 AND c2) OR c3)", never "(c1 AND (c2 OR c3))") — semantic equivalence
// is not enough, the parenthesization itself must be exact.
func TestBuildPolicySQL_ThreeClauseFoldExactParenthesization(t *testing.T) {
	def := PolicyDef{
		Name:   "mixed_and_or",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "requester_id", Operator: "!=", ValueSource: "claim", Value: "sub"},
			{Column: "approved_by", Operator: "IS NULL", Logic: "AND"},
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "active", Logic: "OR"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	roleCheck := `current_setting('app.jwt_role', true) = ANY (ARRAY['member'])`
	clauseExpr := `(("requester_id" != current_setting('app.jwt_sub', true)::UUID AND "approved_by" IS NULL) OR "status" = 'active'::TEXT)`
	wantExpr := "(" + roleCheck + ") AND (" + clauseExpr + ")"
	wantUsing := "USING (" + wantExpr + ")"
	if !strings.Contains(sql, wantUsing) {
		t.Fatalf("sql = %q\nwant it to contain exact fold:\n%q", sql, wantUsing)
	}
}

func TestBuildPolicySQL_FourClauseFoldExactParenthesization(t *testing.T) {
	def := PolicyDef{
		Name:   "four_clauses",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "a"},
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "b", Logic: "OR"},
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "c", Logic: "AND"},
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "d", Logic: "OR"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c1 := `"status" = 'a'::TEXT`
	c2 := `"status" = 'b'::TEXT`
	c3 := `"status" = 'c'::TEXT`
	c4 := `"status" = 'd'::TEXT`
	roleCheck := `current_setting('app.jwt_role', true) = ANY (ARRAY['member'])`
	clauseExpr := "(((" + c1 + " OR " + c2 + ") AND " + c3 + ") OR " + c4 + ")"
	wantExpr := "(" + roleCheck + ") AND (" + clauseExpr + ")"
	wantUsing := "USING (" + wantExpr + ")"
	if !strings.Contains(sql, wantUsing) {
		t.Fatalf("sql = %q\nwant it to contain exact fold:\n%q", sql, wantUsing)
	}
}

func TestBuildPolicySQL_FirstClauseWithLogicIsRejected(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "a", Logic: "AND"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for first clause carrying logic, got nil")
	}
}

func TestBuildPolicySQL_NonFirstClauseMissingLogicIsRejected(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "a"},
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "b"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for second clause missing logic, got nil")
	}
}

func TestBuildPolicySQL_NonFirstClauseInvalidLogicIsRejected(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "a"},
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "b", Logic: "XOR"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for invalid logic value, got nil")
	}
}

func TestBuildPolicySQL_MultipleRolesUseAnyArray(t *testing.T) {
	def := PolicyDef{
		Name:   "multi_role",
		Action: "select",
		Roles:  []string{"approver", "admin"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}
	sql, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `current_setting('app.jwt_role', true) = ANY (ARRAY['approver', 'admin'])`
	if !strings.Contains(sql, want) {
		t.Fatalf("sql = %q, want it to contain %q", sql, want)
	}
	if !strings.Contains(sql, "TO zeep_app_enduser") {
		t.Fatalf("sql = %q, want it to target TO zeep_app_enduser (not the business roles)", sql)
	}
}

func TestBuildPolicySQL_RejectsRoleFailingIdentRe(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"bad role; --"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NOT NULL"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for role failing identRe, got nil")
	}
}

func TestBuildPolicySQL_RejectsEmptyRoles(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NULL"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for empty roles, got nil")
	}
}

func TestBuildPolicySQL_RejectsInvalidAction(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "truncate",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "IS NULL"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for invalid action, got nil")
	}
}

func TestBuildPolicySQL_RejectsInvalidValueSource(t *testing.T) {
	def := PolicyDef{
		Name:   "p",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "session_variable", Value: "x"},
		},
	}
	_, err := BuildPolicySQL("app_schema", "requests", def, testColumns())
	if err == nil {
		t.Fatal("expected error for invalid value_source, got nil")
	}
}

func TestQuoteLiteral(t *testing.T) {
	cases := map[string]string{
		"active": "'active'",
		"it's":   "'it''s'",
		`a\b`:    `E'a\\b'`,
		`'; --`:  `'''; --'`,
		`\'; --`: `E'\\''; --'`,
	}
	for in, want := range cases {
		got := quoteLiteral(in)
		if got != want {
			t.Fatalf("quoteLiteral(%q) = %q, want %q", in, got, want)
		}
	}
}
