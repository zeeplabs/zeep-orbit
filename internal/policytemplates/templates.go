// Package policytemplates is the server-side, pure-Go port of
// internal/dashboard/ui/src/lib/policyTemplates.ts, so orbit_create_policy_
// from_template (mcp-server spec, T12) can build PolicyDefs without a
// browser.
//
// KEEP THESE IN SYNC MANUALLY — see
// internal/dashboard/ui/src/lib/policyTemplates.ts and
// .specs/features/mcp-server/design.md's Risks & Concerns table. No shared
// codegen source exists between the TS and Go versions in V1; a change to
// either file must be mirrored in the other by hand.
//
// This package has no dependency on internal/dashboard — PolicyDef/
// PolicyClause below are a deliberate, field-for-field mirror of
// dashboard.PolicyDef/dashboard.PolicyClause (and the TS file's own
// PolicyDef/PolicyClause from src/lib/api.ts), not an import of them, so
// this package stays pure per design.md's Component note ("Dependencies:
// none"). Callers that hand these into CreateTablePolicyForUser (T12)
// convert field-by-field — the shapes are identical.
package policytemplates

// PolicyClause mirrors dashboard.PolicyClause / the TS file's PolicyClause.
type PolicyClause struct {
	Column      string
	Operator    string
	ValueSource string
	Value       string
	Logic       string
}

// PolicyDef mirrors dashboard.PolicyDef / the TS file's PolicyDef.
type PolicyDef struct {
	Name    string
	Action  string
	Roles   []string
	Clauses []PolicyClause
}

// TemplateID constants — same 6 values as the TS file's TemplateId union.
const (
	TemplateIDOwnerOnly          = "owner_only"
	TemplateIDOpenRead           = "open_read"
	TemplateIDReadOnly           = "read_only"
	TemplateIDValueMatch         = "value_match"
	TemplateIDOpenReadOwnerWrite = "open_read_owner_write"
	TemplateIDBlockedByDefault   = "blocked_by_default"
)

// TemplateDefinition mirrors the TS file's TemplateDefinition interface.
// ActionsFixed is nil when the user chooses the actions (owner_only), same
// as the TS type's optional actionsFixed?: string[].
type TemplateDefinition struct {
	ID                  string
	RequiresOwnerColumn bool
	Kind                string // "single" | "composite" | "info"
	ActionsFixed        []string
}

// List returns the same 6 templates, in the same order, as the TS file's
// TEMPLATE_DEFINITIONS array.
func List() []TemplateDefinition {
	return []TemplateDefinition{
		{ID: TemplateIDOwnerOnly, RequiresOwnerColumn: true, Kind: "single"},
		{ID: TemplateIDOpenRead, RequiresOwnerColumn: false, Kind: "single", ActionsFixed: []string{"select"}},
		{ID: TemplateIDReadOnly, RequiresOwnerColumn: false, Kind: "single", ActionsFixed: []string{"select"}},
		{ID: TemplateIDValueMatch, RequiresOwnerColumn: false, Kind: "single", ActionsFixed: []string{"select"}},
		{ID: TemplateIDOpenReadOwnerWrite, RequiresOwnerColumn: true, Kind: "composite", ActionsFixed: []string{"select", "update", "delete"}},
		{ID: TemplateIDBlockedByDefault, RequiresOwnerColumn: false, Kind: "info"},
	}
}

// GeneratedPolicyName mirrors generatedPolicyName — the single point that
// decides the Name sent to the backend for a template-generated policy.
func GeneratedPolicyName(templateID, action string) string {
	return "tpl_" + templateID + "_" + action
}

// openReadClause mirrors OPEN_READ_CLAUSE: a dummy always-true clause for
// templates that don't filter rows (owner_id IS NOT NULL is true on every
// table where hasOwnerColumn(rls) is true), satisfying BuildPolicySQL's
// len(Clauses)==0 rejection without ever filtering a row.
var openReadClause = PolicyClause{Column: "owner_id", Operator: "IS NOT NULL"}

// BuildOwnerOnlyPolicies mirrors buildOwnerOnlyPolicies: one PolicyDef per
// action, each scoped to "owner_id = <jwt sub claim>".
func BuildOwnerOnlyPolicies(actions, roles []string) []PolicyDef {
	defs := make([]PolicyDef, 0, len(actions))
	for _, action := range actions {
		defs = append(defs, PolicyDef{
			Name:   GeneratedPolicyName(TemplateIDOwnerOnly, action),
			Action: action,
			Roles:  roles,
			Clauses: []PolicyClause{
				{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"},
			},
		})
	}
	return defs
}

// BuildOpenReadPolicy mirrors buildOpenReadPolicy: select, open to the
// chosen roles, no row filter (dummy clause).
func BuildOpenReadPolicy(roles []string) PolicyDef {
	return PolicyDef{
		Name:    GeneratedPolicyName(TemplateIDOpenRead, "select"),
		Action:  "select",
		Roles:   roles,
		Clauses: []PolicyClause{openReadClause},
	}
}

// BuildReadOnlyPolicy mirrors buildReadOnlyPolicy: same shape as
// BuildOpenReadPolicy — the template is a distinct product-facing entry
// ("nobody can write"), not a distinct technical shape.
func BuildReadOnlyPolicy(roles []string) PolicyDef {
	policy := BuildOpenReadPolicy(roles)
	policy.Name = GeneratedPolicyName(TemplateIDReadOnly, "select")
	return policy
}

// BuildValueMatchPolicy mirrors buildValueMatchPolicy: select, filtered to
// rows where a real column equals a literal value chosen by the user.
func BuildValueMatchPolicy(column, value string, roles []string) PolicyDef {
	return PolicyDef{
		Name:   GeneratedPolicyName(TemplateIDValueMatch, "select"),
		Action: "select",
		Roles:  roles,
		Clauses: []PolicyClause{
			{Column: column, Operator: "=", ValueSource: "literal", Value: value},
		},
	}
}

// BuildOpenReadOwnerWritePolicies mirrors buildOpenReadOwnerWritePolicies:
// open read for readRoles, write (update/delete) restricted to the row's
// owner. Reuses BuildOpenReadPolicy/BuildOwnerOnlyPolicies' clause shapes —
// only the generated Name is rebased onto this template's id.
func BuildOpenReadOwnerWritePolicies(readRoles []string) []PolicyDef {
	selectPolicy := BuildOpenReadPolicy(readRoles)
	writePolicies := BuildOwnerOnlyPolicies([]string{"update", "delete"}, readRoles)

	result := make([]PolicyDef, 0, 1+len(writePolicies))
	selectPolicy.Name = GeneratedPolicyName(TemplateIDOpenReadOwnerWrite, selectPolicy.Action)
	result = append(result, selectPolicy)
	for _, p := range writePolicies {
		p.Name = GeneratedPolicyName(TemplateIDOpenReadOwnerWrite, p.Action)
		result = append(result, p)
	}
	return result
}
