package dashboard

// builder_policy.go — the add_policy operation of the AI Frontend Builder
// (ai-builder-backend B04, BBE-11): a row policy built from one of the named
// policy templates, through the same CreateTablePolicyForUser path (admin
// only, validation, audit) as the dashboard and the MCP tool.

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard/ai"
	"github.com/zeeplabs/zeep-orbit/internal/policytemplates"
)

// builderPolicyAdminMsg is shown to the model and the user when the caller is
// not an admin of the backend app: policies decide who can read and write
// rows, so creating one needs the same role as in the dashboard.
const builderPolicyAdminMsg = "creating a row policy needs the admin role on the backend app."

// resolveBuilderPolicies turns an add_policy operation into the policies its
// template produces. Errors are safe to show: they name the missing input or
// the unusable template, never a Go error.
func resolveBuilderPolicies(op *ai.PlanPolicyOp) ([]policytemplates.PolicyDef, error) {
	return policytemplates.Resolve(policytemplates.ResolveInput{
		TemplateID: op.Template,
		Roles:      op.Roles,
		Actions:    op.Actions,
		Column:     op.Column,
		Value:      op.Value,
	})
}

// toDashboardPolicyDef copies a template policy into the dashboard type (the
// two are a deliberate field-for-field mirror, see the policytemplates package
// doc).
func toDashboardPolicyDef(p policytemplates.PolicyDef) PolicyDef {
	clauses := make([]PolicyClause, 0, len(p.Clauses))
	for _, c := range p.Clauses {
		clauses = append(clauses, PolicyClause{
			Column: c.Column, Operator: c.Operator, ValueSource: c.ValueSource, Value: c.Value, Logic: c.Logic,
		})
	}
	return PolicyDef{Name: p.Name, Action: p.Action, Roles: p.Roles, Clauses: clauses}
}

// policyApplyError reports that one policy of a multi-policy template failed
// after Created earlier ones were already in place. The earlier ones stay: a
// template such as open_read_owner_write creates three policies, and each is
// its own provisioner call.
type policyApplyError struct {
	Policy  string // name of the policy that failed
	Created int    // policies of this operation created before it
	Total   int
	Err     error
}

func (e *policyApplyError) Error() string {
	return fmt.Sprintf("dashboard: policy %q failed after %d of %d: %v", e.Policy, e.Created, e.Total, e.Err)
}

func (e *policyApplyError) Unwrap() error { return e.Err }

// applyAddPolicy creates every policy of op's template on its table, in order,
// stopping at the first failure.
func (h *Handler) applyAddPolicy(ctx context.Context, user *DashboardUser, appID string, op *ai.PlanPolicyOp, origin string) error {
	defs, err := resolveBuilderPolicies(op)
	if err != nil {
		return &ValidationError{msg: builderPolicyInputMessage(err)}
	}
	// Every template filters on the owner_id column, which exists only once the
	// table has an access mode. Without this check the failure would surface as
	// an untyped DDL error from Postgres; the agent needs to be told to set the
	// mode first.
	app, _, err := GetApp(ctx, h.pool, appID, user)
	if err != nil {
		return err
	}
	table := findAppTableByName(app, op.Table)
	if table == nil {
		return ErrTableNotFound
	}
	if !config.HasOwnerColumn(table.RLS) {
		return &ValidationError{msg: fmt.Sprintf(
			"table %q has no access mode yet, so a policy cannot be created; set it first with set_rls_mode (\"policy\" for policy templates).", op.Table)}
	}
	for i, def := range defs {
		if _, err := h.CreateTablePolicyForUser(ctx, user, appID, op.Table, toDashboardPolicyDef(def), origin); err != nil {
			return &policyApplyError{Policy: def.Name, Created: i, Total: len(defs), Err: err}
		}
	}
	return nil
}

// builderPolicyInputMessage turns a Resolve failure into a sentence for the
// model and the user.
func builderPolicyInputMessage(err error) string {
	var missing *policytemplates.MissingInputError
	switch {
	case errors.Is(err, policytemplates.ErrNoPolicyTemplate):
		return "the blocked_by_default template creates no policy; set the table's access mode with set_rls_mode instead."
	case errors.As(err, &missing):
		if missing.Field == "template_id" {
			return "the policy template is unknown; use one of owner_only, open_read, read_only, value_match or open_read_owner_write."
		}
		return fmt.Sprintf("this policy template needs %q.", missing.Field)
	}
	return "the policy template could not be resolved."
}

// builderPolicyPartial reports whether err left some policies of an add_policy
// operation in place, so the model is not told that nothing changed.
func builderPolicyPartial(err error) bool {
	var pe *policyApplyError
	return errors.As(err, &pe) && pe.Created > 0
}
