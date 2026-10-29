package policytemplates

import "errors"

// ResolveInput is what a caller supplies to turn a named template into
// concrete policies. Which fields a template needs is the same as for
// orbit_create_policy_from_template: owner_only takes roles and actions,
// value_match takes roles, column and value, the others take roles.
type ResolveInput struct {
	TemplateID string
	Roles      []string
	Actions    []string
	Column     string
	Value      string
}

// MissingInputError names the input a template call lacks (or the template id
// itself when it is unknown). Field is safe to show to a user or a model.
type MissingInputError struct{ Field string }

func (e *MissingInputError) Error() string { return "missing or invalid input: " + e.Field }

// ErrNoPolicyTemplate is returned for blocked_by_default: that template is
// informational and creates no policy.
var ErrNoPolicyTemplate = errors.New("template blocked_by_default creates no policy — set the table's rls mode instead")

// Resolve builds the policies a template produces for in, or a
// *MissingInputError when a required input is empty, before anything is
// created. It mirrors the template switch of the MCP tool
// orbit_create_policy_from_template (internal/mcpserver) so the AI builder
// accepts the same inputs; the MCP tool still carries its own copy.
func Resolve(in ResolveInput) ([]PolicyDef, error) {
	switch in.TemplateID {
	case TemplateIDOwnerOnly:
		if len(in.Roles) == 0 {
			return nil, &MissingInputError{"roles"}
		}
		if len(in.Actions) == 0 {
			return nil, &MissingInputError{"actions"}
		}
		return BuildOwnerOnlyPolicies(in.Actions, in.Roles), nil
	case TemplateIDOpenRead:
		if len(in.Roles) == 0 {
			return nil, &MissingInputError{"roles"}
		}
		return []PolicyDef{BuildOpenReadPolicy(in.Roles)}, nil
	case TemplateIDReadOnly:
		if len(in.Roles) == 0 {
			return nil, &MissingInputError{"roles"}
		}
		return []PolicyDef{BuildReadOnlyPolicy(in.Roles)}, nil
	case TemplateIDValueMatch:
		if len(in.Roles) == 0 {
			return nil, &MissingInputError{"roles"}
		}
		if in.Column == "" {
			return nil, &MissingInputError{"column"}
		}
		if in.Value == "" {
			return nil, &MissingInputError{"value"}
		}
		return []PolicyDef{BuildValueMatchPolicy(in.Column, in.Value, in.Roles)}, nil
	case TemplateIDOpenReadOwnerWrite:
		if len(in.Roles) == 0 {
			return nil, &MissingInputError{"roles"}
		}
		return BuildOpenReadOwnerWritePolicies(in.Roles), nil
	case TemplateIDBlockedByDefault:
		return nil, ErrNoPolicyTemplate
	default:
		return nil, &MissingInputError{"template_id"}
	}
}
