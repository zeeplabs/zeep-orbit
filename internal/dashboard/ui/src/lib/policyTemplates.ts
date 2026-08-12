import { PolicyClause, PolicyDef } from "./api";

// Pure data + builders for the policy template picker (PTPL-01..08). No JSX,
// no network calls — every function here maps a minimal user input to the
// PolicyDef shape internal/provisioner/policy.go's BuildPolicySQL accepts,
// so a non-technical Dashboard user never has to pick Column/Operator/
// ValueSource/Logic directly.

export type TemplateId =
  | "owner_only"
  | "open_read"
  | "read_only"
  | "value_match"
  | "open_read_owner_write"
  | "blocked_by_default";

export interface TemplateDefinition {
  id: TemplateId;
  requiresOwnerColumn: boolean;
  kind: "single" | "composite" | "info";
  /** Actions the template always creates. Absent when the user chooses (owner_only). */
  actionsFixed?: string[];
}

// Ordered per spec.md AC P1-1: owner_only, open_read, read_only, value_match,
// blocked_by_default (open_read_owner_write is appended by T3).
export const TEMPLATE_DEFINITIONS: TemplateDefinition[] = [
  { id: "owner_only", requiresOwnerColumn: true, kind: "single" },
  { id: "open_read", requiresOwnerColumn: false, kind: "single", actionsFixed: ["select"] },
  { id: "read_only", requiresOwnerColumn: false, kind: "single", actionsFixed: ["select"] },
  { id: "value_match", requiresOwnerColumn: false, kind: "single", actionsFixed: ["select"] },
  { id: "open_read_owner_write", requiresOwnerColumn: true, kind: "composite", actionsFixed: ["select", "update", "delete"] },
  { id: "blocked_by_default", requiresOwnerColumn: false, kind: "info" },
];

// generatedPolicyName is the single point that decides the Name sent to the
// backend for a template-generated policy — never typed by the user in
// template mode (spec Assumptions table).
export function generatedPolicyName(templateId: string, action: string): string {
  return `tpl_${templateId}_${action}`;
}

// Dummy always-true clause for templates that don't filter rows (PTPL-02,
// PTPL-04): owner_id is NOT NULL on every table where hasOwnerColumn(rls) is
// true, so this clause satisfies BuildPolicySQL's len(Clauses)==0 rejection
// without ever filtering a row or being shown to the user as a "condition".
const OPEN_READ_CLAUSE: PolicyClause = { column: "owner_id", operator: "IS NOT NULL" };

// buildOwnerOnlyPolicies (PTPL-01): one PolicyDef per action, each scoped to
// "owner_id = <jwt sub claim>".
export function buildOwnerOnlyPolicies(actions: string[], roles: string[]): PolicyDef[] {
  return actions.map((action) => ({
    name: generatedPolicyName("owner_only", action),
    action,
    roles,
    clauses: [{ column: "owner_id", operator: "=", value_source: "claim", value: "sub" }],
  }));
}

// buildOpenReadPolicy (PTPL-02): select, open to the chosen roles, no row
// filter (dummy clause).
export function buildOpenReadPolicy(roles: string[]): PolicyDef {
  return {
    name: generatedPolicyName("open_read", "select"),
    action: "select",
    roles,
    clauses: [OPEN_READ_CLAUSE],
  };
}

// buildReadOnlyPolicy (PTPL-04): same shape as buildOpenReadPolicy — the
// template exists as a distinct product-facing entry ("nobody can write"),
// not a distinct technical shape.
export function buildReadOnlyPolicy(roles: string[]): PolicyDef {
  const policy = buildOpenReadPolicy(roles);
  return { ...policy, name: generatedPolicyName("read_only", "select") };
}

// buildValueMatchPolicy (PTPL-05): select, filtered to rows where a real
// column equals a literal value chosen by the user.
export function buildValueMatchPolicy(column: string, value: string, roles: string[]): PolicyDef {
  return {
    name: generatedPolicyName("value_match", "select"),
    action: "select",
    roles,
    clauses: [{ column, operator: "=", value_source: "literal", value }],
  };
}

// buildOpenReadOwnerWritePolicies (PTPL-06): open read for readRoles, write
// (update/delete) restricted to the row's owner. Reuses buildOpenReadPolicy
// and buildOwnerOnlyPolicies' clause shapes — only the generated Name is
// rebased onto this template's id, since each of the 3 policies belongs to
// the composite template, not to open_read/owner_only individually.
export function buildOpenReadOwnerWritePolicies(readRoles: string[]): PolicyDef[] {
  const select = buildOpenReadPolicy(readRoles);
  const [update, del] = buildOwnerOnlyPolicies(["update", "delete"], readRoles);
  return [select, update, del].map((policy) => ({
    ...policy,
    name: generatedPolicyName("open_read_owner_write", policy.action),
  }));
}
