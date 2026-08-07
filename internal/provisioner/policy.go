package provisioner

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// identRe mirrors internal/dashboard/handler.go's identRe (same allowlist
// pattern for column/policy identifiers). It cannot be imported directly:
// dashboard already imports provisioner, so importing dashboard here would
// create a cycle. Keep both regexes identical if either changes.
var identRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// policyOperators is the full allowlist of comparison operators a policy
// clause may use. Anything outside this set is rejected before any DDL is
// built (spec ROWPOL-07/28).
var policyOperators = map[string]string{
	"=":           "=",
	"!=":          "!=",
	"IN":          "IN",
	"NOT IN":      "NOT IN",
	">":           ">",
	"<":           "<",
	">=":          ">=",
	"<=":          "<=",
	"IS NULL":     "IS NULL",
	"IS NOT NULL": "IS NOT NULL",
}

// unaryOperators never take an operand (spec ROWPOL-09 edge case).
var unaryOperators = map[string]bool{
	"IS NULL":     true,
	"IS NOT NULL": true,
}

// allowedClaims is the fixed set of JWT claims a clause may reference via
// value_source: "claim" — the only identity data the JWT carries (spec
// Assumptions table).
var allowedClaims = map[string]bool{
	"role":  true,
	"sub":   true,
	"email": true,
}

// claimGUC maps a claim name to the session GUC set by db.Pool.WithRLSContext
// (internal/db/client.go) — must stay in sync with that method's GUC names.
var claimGUC = map[string]string{
	"role":  "app.jwt_role",
	"sub":   "app.jwt_sub",
	"email": "app.jwt_email",
}

// allowedActions maps 1:1 to Postgres RLS command kinds (spec: "Granularidade
// de ação").
var allowedActions = map[string]bool{
	"select": true,
	"insert": true,
	"update": true,
	"delete": true,
}

var allowedLogic = map[string]bool{
	"AND": true,
	"OR":  true,
}

// PolicyClause is one structured condition inside a PolicyDef. Every clause
// but the first must carry Logic ("AND"/"OR") connecting it to the fold
// accumulated so far (spec: composição de cláusulas).
type PolicyClause struct {
	Column      string
	Operator    string
	ValueSource string // "claim" | "literal" — empty for unary operators
	Value       string // claim name ("role"/"sub"/"email") or literal value(s); empty for unary operators. IN/NOT IN take a comma-separated list.
	Logic       string // "AND" | "OR" — empty only on the first clause of Clauses
}

// PolicyDef is the full structured definition of one native Postgres RLS
// policy, translated to SQL by BuildPolicySQL.
type PolicyDef struct {
	Name    string // becomes the CREATE POLICY name — must itself satisfy identRe
	Action  string // select|insert|update|delete
	Roles   []string
	Clauses []PolicyClause
}

// BuildPolicySQL validates def against tableColumns and translates it into a
// complete `CREATE POLICY ... TO zeep_app_enduser USING (...) [WITH CHECK
// (...)]` statement. It never executes DDL itself and never concatenates raw
// user input into the returned SQL without going through quoteLiteral or the
// fixed identRe/operator/claim allowlists — every rejection happens before
// any SQL is assembled (spec ROWPOL-07/08/09).
func BuildPolicySQL(schema, table string, def PolicyDef, tableColumns []config.ColumnConfig) (string, error) {
	if !identRe.MatchString(def.Name) {
		return "", fmt.Errorf("policy: invalid policy name %q", def.Name)
	}
	if !allowedActions[def.Action] {
		return "", fmt.Errorf("policy: invalid action %q", def.Action)
	}
	if len(def.Roles) == 0 {
		return "", fmt.Errorf("policy: at least one role is required")
	}
	for _, role := range def.Roles {
		if role == "" {
			return "", fmt.Errorf("policy: role must not be empty")
		}
	}
	if len(def.Clauses) == 0 {
		return "", fmt.Errorf("policy: at least one clause is required")
	}

	colByName := make(map[string]config.ColumnConfig, len(tableColumns))
	for _, c := range tableColumns {
		colByName[c.Name] = c
	}

	expr, err := foldClauses(def.Clauses, colByName)
	if err != nil {
		return "", err
	}

	roleList := make([]string, len(def.Roles))
	for i, role := range def.Roles {
		if !identRe.MatchString(role) {
			return "", fmt.Errorf("policy: invalid role %q", role)
		}
		roleList[i] = fmt.Sprintf("%q", role)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE POLICY %q ON %q.%q FOR %s TO %s USING (%s)",
		def.Name, schema, table, strings.ToUpper(def.Action), strings.Join(roleList, ", "), expr)
	if def.Action == "insert" || def.Action == "update" {
		fmt.Fprintf(&sb, " WITH CHECK (%s)", expr)
	}

	return sb.String(), nil
}

// foldClauses validates every clause, translates each to a parenthesized SQL
// fragment, and folds them left-to-right by each clause's Logic, fully
// parenthesizing at every step so the result never depends on SQL operator
// precedence between AND/OR (spec: "((c1 AND c2) OR c3)", never
// "(c1 AND (c2 OR c3))").
func foldClauses(clauses []PolicyClause, colByName map[string]config.ColumnConfig) (string, error) {
	acc := ""
	for i, clause := range clauses {
		if i == 0 {
			if clause.Logic != "" {
				return "", fmt.Errorf("policy: clause 1 must not have logic (no accumulated clause to connect to)")
			}
		} else if !allowedLogic[clause.Logic] {
			return "", fmt.Errorf("policy: clause %d has invalid logic %q, must be AND or OR", i+1, clause.Logic)
		}

		frag, err := translateClause(clause, colByName)
		if err != nil {
			return "", fmt.Errorf("policy: clause %d: %w", i+1, err)
		}

		if i == 0 {
			acc = frag
			continue
		}
		acc = fmt.Sprintf("(%s %s %s)", acc, clause.Logic, frag)
	}
	return acc, nil
}

// translateClause validates and translates a single PolicyClause into a SQL
// boolean fragment (without surrounding fold parentheses).
func translateClause(clause PolicyClause, colByName map[string]config.ColumnConfig) (string, error) {
	if !identRe.MatchString(clause.Column) {
		return "", fmt.Errorf("invalid column name %q", clause.Column)
	}
	col, ok := colByName[clause.Column]
	if !ok {
		return "", fmt.Errorf("unknown column %q", clause.Column)
	}
	sqlOp, ok := policyOperators[clause.Operator]
	if !ok {
		return "", fmt.Errorf("invalid operator %q", clause.Operator)
	}

	if unaryOperators[clause.Operator] {
		if clause.ValueSource != "" || clause.Value != "" {
			return "", fmt.Errorf("operator %q is unary and must not have value_source/value", clause.Operator)
		}
		return fmt.Sprintf("%q %s", clause.Column, sqlOp), nil
	}

	operand, err := translateOperand(clause, col)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%q %s %s", clause.Column, sqlOp, operand), nil
}

// translateOperand builds the right-hand side of a non-unary clause: a
// current_setting(...) expression for value_source "claim", or a
// quote_literal-escaped literal for value_source "literal" — never raw user
// input concatenated into SQL. IN/NOT IN operators take a comma-separated
// Value and produce a parenthesized list; every other operator takes Value
// as a single operand.
func translateOperand(clause PolicyClause, col config.ColumnConfig) (string, error) {
	isList := clause.Operator == "IN" || clause.Operator == "NOT IN"

	switch clause.ValueSource {
	case "claim":
		if !allowedClaims[clause.Value] && !isList {
			return "", fmt.Errorf("invalid claim %q, must be role, sub, or email", clause.Value)
		}
		if isList {
			parts := splitValueList(clause.Value)
			if len(parts) == 0 {
				return "", fmt.Errorf("value must not be empty")
			}
			exprs := make([]string, len(parts))
			for i, p := range parts {
				if !allowedClaims[p] {
					return "", fmt.Errorf("invalid claim %q, must be role, sub, or email", p)
				}
				exprs[i] = claimExpr(p, col)
			}
			return "(" + strings.Join(exprs, ", ") + ")", nil
		}
		return claimExpr(clause.Value, col), nil
	case "literal":
		if isList {
			parts := splitValueList(clause.Value)
			if len(parts) == 0 {
				return "", fmt.Errorf("value must not be empty")
			}
			exprs := make([]string, len(parts))
			for i, p := range parts {
				exprs[i] = literalExpr(p, col)
			}
			return "(" + strings.Join(exprs, ", ") + ")", nil
		}
		if clause.Value == "" {
			return "", fmt.Errorf("value must not be empty")
		}
		return literalExpr(clause.Value, col), nil
	default:
		return "", fmt.Errorf("invalid value_source %q, must be claim or literal", clause.ValueSource)
	}
}

func splitValueList(value string) []string {
	raw := strings.Split(value, ",")
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// claimExpr builds the current_setting(...) expression for a claim, cast to
// the referencing column's Postgres type.
func claimExpr(claim string, col config.ColumnConfig) string {
	return fmt.Sprintf("current_setting('%s', true)::%s", claimGUC[claim], pgType(col.Type))
}

// literalExpr safely embeds a user-supplied literal via quoteLiteral, cast to
// the referencing column's Postgres type.
func literalExpr(value string, col config.ColumnConfig) string {
	return fmt.Sprintf("%s::%s", quoteLiteral(value), pgType(col.Type))
}

// quoteLiteral escapes value for safe embedding as a Postgres string literal,
// following the same rule Postgres's own quote_literal() applies: double
// every single quote, and — if the value contains a backslash — use the E”
// escape-string form with backslashes doubled too, so the result is safe
// regardless of the session's standard_conforming_strings setting. This is a
// pure-Go equivalent (no DB round-trip) so BuildPolicySQL stays usable from a
// unit test with no Postgres connection; it never concatenates the raw value
// into SQL without this escaping.
func quoteLiteral(value string) string {
	hasBackslash := strings.Contains(value, `\`)
	escaped := strings.ReplaceAll(value, "'", "''")
	if hasBackslash {
		escaped = strings.ReplaceAll(escaped, `\`, `\\`)
		return "E'" + escaped + "'"
	}
	return "'" + escaped + "'"
}
