package dashboard

// builder_plan.go — propose_backend_plan, the AI Frontend Builder's way to
// design a backend in one go (ai-builder-backend B05, BBE-05/BBE-06): an
// ordered list of the same operations propose_backend_change takes, approved
// by the user once. This file validates a plan; it applies nothing.
//
// The tool is registered together with its confirm path (B06), so no build of
// the product shows the model a proposal the user cannot confirm.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard/ai"
)

// ToolProposeBackendPlan is the name of the plan tool.
const ToolProposeBackendPlan = "propose_backend_plan"

const (
	// builderPlanMaxOps bounds a plan: the card, the apply time and a runaway
	// model (spec assumption 4).
	builderPlanMaxOps       = 30
	builderPlanMaxSummary   = 500
	builderAccessReasonMax  = 200
	builderAccessPublic     = "public"
	builderAccessOwner      = "owner"
	builderAccessPolicy     = "policy"
	builderAccessPolicyMode = "policy"
)

// BuilderPlanOp is one operation of a plan. Access and AccessReason are set
// for add_table only: the agent's declared decision about who can see the
// rows of the table and why (ai-builder-backend BBE-06, BBE-12).
type BuilderPlanOp struct {
	Op           *ai.EditOperation
	Access       string
	AccessReason string
}

// BuilderPlan is a parsed propose_backend_plan call.
type BuilderPlan struct {
	Summary string
	Ops     []BuilderPlanOp
}

// ParseBuilderPlan validates the raw arguments of a propose_backend_plan call
// without looking at the database: size limits, each operation (through the
// same validator as the single-change tool) and the access declaration of every
// new table. Every failure is a *tools.ToolError the model can act on, naming
// the offending operation by its position; nothing from the Go decoder is
// echoed beyond tools.DecodeArgs' sanitized messages. Confirm re-parses the
// stored arguments with this function so what is applied is what was validated.
func ParseBuilderPlan(raw json.RawMessage) (*BuilderPlan, error) {
	var args struct {
		Summary    string            `json:"summary"`
		Operations []json.RawMessage `json:"operations"`
	}
	if err := tools.DecodeArgs(raw, &args); err != nil {
		return nil, err
	}
	summary := strings.TrimSpace(args.Summary)
	if summary == "" {
		return nil, tools.ToolErrorf("invalid arguments: \"summary\" is required: one or two sentences saying what this plan builds")
	}
	if n := len([]rune(summary)); n > builderPlanMaxSummary {
		return nil, tools.ToolErrorf("invalid arguments: \"summary\" is %d characters, the limit is %d", n, builderPlanMaxSummary)
	}
	if len(args.Operations) == 0 {
		return nil, tools.ToolErrorf("invalid arguments: \"operations\" must list at least one operation")
	}
	if len(args.Operations) > builderPlanMaxOps {
		return nil, tools.ToolErrorf("invalid arguments: a plan has at most %d operations, got %d; split it and propose the rest after this one is applied", builderPlanMaxOps, len(args.Operations))
	}

	plan := &BuilderPlan{Summary: summary, Ops: make([]BuilderPlanOp, 0, len(args.Operations))}
	for i, rawOp := range args.Operations {
		op, err := parseBuilderOperation(rawOp)
		if err != nil {
			var te *tools.ToolError
			if errors.As(err, &te) {
				return nil, tools.ToolErrorf("operations[%d]: %s", i, te.Msg)
			}
			return nil, err
		}
		po := BuilderPlanOp{Op: op}
		if op.Kind == "add_table" {
			access, reason, err := parseTableAccess(rawOp)
			if err != nil {
				return nil, tools.ToolErrorf("operations[%d] (add_table %q): %s", i, op.AddTable.Name, err.Error())
			}
			po.Access, po.AccessReason = access, reason
		}
		plan.Ops = append(plan.Ops, po)
	}
	return plan, nil
}

// parseTableAccess reads the access declaration of an add_table operation: who
// can see the rows (public, owner or policy) and a one-line reason. Declaring
// it is mandatory so a table is never public by omission.
func parseTableAccess(operation json.RawMessage) (access, reason string, err error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(operation, &fields); err != nil {
		return "", "", errors.New("the operation must be an object")
	}
	var decl struct {
		Access       string `json:"access"`
		AccessReason string `json:"access_reason"`
	}
	if err := json.Unmarshal(fields["add_table"], &decl); err != nil {
		return "", "", errors.New("\"access\" and \"access_reason\" must be strings")
	}
	access = strings.TrimSpace(decl.Access)
	switch access {
	case builderAccessPublic, builderAccessOwner, builderAccessPolicy:
	case "":
		return "", "", errors.New("\"access\" is required: public (anyone with access to the app reads every row), owner (each user sees only their own rows) or policy (a row policy decides)")
	default:
		return "", "", errors.New("\"access\" must be public, owner or policy")
	}
	reason = strings.TrimSpace(decl.AccessReason)
	if reason == "" {
		return "", "", errors.New("\"access_reason\" is required: one line saying why this table has that access")
	}
	if n := len([]rune(reason)); n > builderAccessReasonMax {
		return "", "", fmt.Errorf("\"access_reason\" is %d characters, the limit is %d", n, builderAccessReasonMax)
	}
	return access, reason, nil
}

// Validate walks the plan in order against the tables the backend has now and
// fails on the first operation that could not apply: a table that already
// exists, a table, column or reference that exists neither today nor earlier in
// the plan, an invalid column or index, a policy on a table with no access mode
// at that point, or a table whose declared access contradicts what the plan
// does to it. tables is not modified; authEmail is the app's current email
// auth flag. Nothing is written.
//
// It does not model system_config.require_rls_default, which can give a new
// table a restricted access mode when none is requested; apply-time behavior
// for that case is covered with the apply path (B06).
func (p *BuilderPlan) Validate(tables []AppTableRow, authEmail bool) error {
	state := make([]AppTableRow, len(tables))
	copy(state, tables)

	type created struct {
		access   string
		policies int
	}
	madeInPlan := map[string]*created{}

	find := func(name string) int {
		for i := range state {
			if state[i].Name == name {
				return i
			}
		}
		return -1
	}
	othersOf := func(skip int) []AppTableRow {
		out := make([]AppTableRow, 0, len(state))
		for i := range state {
			if i != skip {
				out = append(out, state[i])
			}
		}
		return out
	}
	// revalidate checks the table at idx as the dashboard would after the
	// change (identifiers, types, duplicates, references and indexes).
	revalidate := func(idx int) error { return validateTableInput(state[idx], authEmail, othersOf(idx)) }

	for i, po := range p.Ops {
		op := po.Op
		fail := func(format string, a ...any) error {
			return tools.ToolErrorf("operations[%d] (%s): %s", i, op.Kind, fmt.Sprintf(format, a...))
		}
		missing := func(table string) error {
			return fail("table %q does not exist yet; add it earlier in the plan or check orbit_get_app_schema", table)
		}

		switch op.Kind {
		case "add_table":
			row := AppTableRow{Name: op.AddTable.Name, Columns: planColumnsToConfig(op.AddTable.Columns)}
			if err := validateTableInput(row, authEmail, state); err != nil {
				return fail("%s", err.Error())
			}
			state = append(state, row)
			madeInPlan[row.Name] = &created{access: po.Access}

		case "add_column":
			idx := find(op.AddColumn.Table)
			if idx < 0 {
				return missing(op.AddColumn.Table)
			}
			state[idx].Columns = appendColumn(state[idx].Columns, config.ColumnConfig{
				Name: op.AddColumn.Column.Name, Type: op.AddColumn.Column.Type, AllowedValues: op.AddColumn.Column.AllowedValues,
			})
			if err := revalidate(idx); err != nil {
				return fail("%s", err.Error())
			}

		case "add_index":
			idx := find(op.AddIndex.Table)
			if idx < 0 {
				return missing(op.AddIndex.Table)
			}
			state[idx].Indexes = append(append([]config.IndexConfig(nil), state[idx].Indexes...),
				config.IndexConfig{Name: op.AddIndex.Name, Columns: op.AddIndex.Columns, Unique: op.AddIndex.Unique})
			if err := revalidate(idx); err != nil {
				return fail("%s", err.Error())
			}

		case "add_reference":
			idx := find(op.AddReference.Table)
			if idx < 0 {
				return missing(op.AddReference.Table)
			}
			state[idx].Columns = appendColumn(state[idx].Columns, config.ColumnConfig{
				Name: op.AddReference.Column.Name, Type: op.AddReference.Column.Type,
				References: &config.ReferenceConfig{Table: op.AddReference.RefTable, Column: op.AddReference.RefColumn, OnDelete: op.AddReference.OnDelete},
			})
			if err := revalidate(idx); err != nil {
				return fail("%s", err.Error())
			}

		case "add_foreign_key":
			idx := find(op.AddForeignKey.Table)
			if idx < 0 {
				return missing(op.AddForeignKey.Table)
			}
			ci := columnIndex(state[idx].Columns, op.AddForeignKey.Column)
			if ci < 0 {
				return fail("column %q does not exist on table %q at this point of the plan", op.AddForeignKey.Column, op.AddForeignKey.Table)
			}
			if state[idx].Columns[ci].References != nil {
				return fail("column %q already has a foreign key", op.AddForeignKey.Column)
			}
			cols := append([]config.ColumnConfig(nil), state[idx].Columns...)
			cols[ci].References = &config.ReferenceConfig{Table: op.AddForeignKey.RefTable, Column: op.AddForeignKey.RefColumn, OnDelete: op.AddForeignKey.OnDelete}
			state[idx].Columns = cols
			if err := revalidate(idx); err != nil {
				return fail("%s", err.Error())
			}

		case "remove_foreign_key":
			idx := find(op.RemoveForeignKey.Table)
			if idx < 0 {
				return missing(op.RemoveForeignKey.Table)
			}
			ci := columnIndex(state[idx].Columns, op.RemoveForeignKey.Column)
			if ci < 0 || state[idx].Columns[ci].References == nil {
				return fail("column %q on table %q has no foreign key to remove at this point of the plan", op.RemoveForeignKey.Column, op.RemoveForeignKey.Table)
			}
			cols := append([]config.ColumnConfig(nil), state[idx].Columns...)
			cols[ci].References = nil
			state[idx].Columns = cols

		case "set_rls_mode":
			idx := find(op.SetRLSMode.Table)
			if idx < 0 {
				return missing(op.SetRLSMode.Table)
			}
			if !config.ValidRLS(op.SetRLSMode.Mode) {
				return fail("access mode %q is invalid (use \"\", \"owner\", \"enabled\" or \"policy\")", op.SetRLSMode.Mode)
			}
			if config.HasOwnerColumn(op.SetRLSMode.Mode) && !authEmail {
				return fail("table %q cannot use a restricted access mode while email authentication is off; add a toggle_auth operation before it", op.SetRLSMode.Table)
			}
			state[idx].RLS = op.SetRLSMode.Mode

		case "toggle_auth":
			authEmail = op.ToggleAuth.EmailEnabled

		case "add_policy":
			idx := find(op.AddPolicy.Table)
			if idx < 0 {
				return missing(op.AddPolicy.Table)
			}
			if !config.HasOwnerColumn(state[idx].RLS) {
				return fail("table %q has no access mode at this point of the plan; add a set_rls_mode operation (\"policy\") for it before this one", op.AddPolicy.Table)
			}
			if _, err := resolveBuilderPolicies(op.AddPolicy); err != nil {
				return fail("%s", builderPolicyInputMessage(err))
			}
			if c := madeInPlan[op.AddPolicy.Table]; c != nil {
				c.policies++
			}
		}
	}

	// The declared access of every table the plan creates must match what the
	// plan does to it: the card shows the declaration to the user, so it has to
	// be true.
	for _, po := range p.Ops {
		if po.Op.Kind != "add_table" {
			continue
		}
		name := po.Op.AddTable.Name
		idx := find(name)
		mode, c := state[idx].RLS, madeInPlan[name]
		var problem string
		switch po.Access {
		case builderAccessPublic:
			if mode != "" || c.policies > 0 {
				problem = fmt.Sprintf("declared public, but the plan gives it an access mode (%q) or a policy; declare owner or policy instead, or remove those operations", mode)
			}
		case builderAccessOwner:
			if mode != "owner" && mode != "enabled" {
				problem = "declared owner, so the plan must set its access mode to \"owner\" (set_rls_mode) after creating it"
			}
		case builderAccessPolicy:
			if mode != builderAccessPolicyMode {
				problem = "declared policy, so the plan must set its access mode to \"policy\" (set_rls_mode) after creating it"
			} else if c.policies == 0 {
				problem = "declared policy, so the plan must include at least one add_policy operation for it"
			}
		}
		if problem != "" {
			return tools.ToolErrorf("table %q is %s", name, problem)
		}
	}
	return nil
}

// appendColumn returns cols plus col without modifying the caller's slice.
func appendColumn(cols []config.ColumnConfig, col config.ColumnConfig) []config.ColumnConfig {
	return append(append([]config.ColumnConfig(nil), cols...), col)
}

func columnIndex(cols []config.ColumnConfig, name string) int {
	for i := range cols {
		if cols[i].Name == name {
			return i
		}
	}
	return -1
}

// proposeBackendPlan is the handler of propose_backend_plan. Like
// propose_backend_change it changes nothing: it validates the plan against the
// backend as it is now and tells the model to stop and wait for the user.
func (b *builderTools) proposeBackendPlan(ctx context.Context, call tools.Call) (tools.Result, error) {
	plan, err := ParseBuilderPlan(call.Arguments)
	if err != nil {
		return tools.Result{}, err
	}
	owner, appID, err := b.ownerAndApp(ctx, call.SessionID)
	if err != nil {
		return tools.Result{}, err
	}
	app, role, err := GetApp(ctx, b.deps.Pool, appID, owner)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return tools.Result{}, tools.ToolErrorf("%s", builderAppUnavailableMsg)
		}
		return tools.Result{}, fmt.Errorf("dashboard: builder tools: get app for plan: %w", err)
	}
	if !role.Effective() {
		return tools.Result{}, tools.ToolErrorf("%s", builderAppUnavailableMsg)
	}
	for _, po := range plan.Ops {
		if po.Op.Kind == "add_policy" && !role.CanManage() {
			return tools.Result{}, tools.ToolErrorf("%s", builderPolicyAdminMsg)
		}
	}
	if err := plan.Validate(app.Tables, app.AuthEmailEnabled); err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Content: fmt.Sprintf(
		"Plan recorded (%d operations) and sent to the user for confirmation. It has NOT been applied. Stop here and wait: the user will confirm or reject the whole plan in the dashboard.", len(plan.Ops))}, nil
}
