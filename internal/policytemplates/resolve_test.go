package policytemplates

import (
	"errors"
	"testing"
)

func TestResolve(t *testing.T) {
	roles := []string{"member"}
	cases := []struct {
		name      string
		in        ResolveInput
		wantNames []string
		wantField string
		wantErr   error
	}{
		{name: "owner only per action", in: ResolveInput{TemplateID: TemplateIDOwnerOnly, Roles: roles, Actions: []string{"select", "update"}},
			wantNames: []string{"tpl_owner_only_select", "tpl_owner_only_update"}},
		{name: "owner only needs roles", in: ResolveInput{TemplateID: TemplateIDOwnerOnly, Actions: []string{"select"}}, wantField: "roles"},
		{name: "owner only needs actions", in: ResolveInput{TemplateID: TemplateIDOwnerOnly, Roles: roles}, wantField: "actions"},
		{name: "open read", in: ResolveInput{TemplateID: TemplateIDOpenRead, Roles: roles}, wantNames: []string{"tpl_open_read_select"}},
		{name: "open read needs roles", in: ResolveInput{TemplateID: TemplateIDOpenRead}, wantField: "roles"},
		{name: "read only", in: ResolveInput{TemplateID: TemplateIDReadOnly, Roles: roles}, wantNames: []string{"tpl_read_only_select"}},
		{name: "value match", in: ResolveInput{TemplateID: TemplateIDValueMatch, Roles: roles, Column: "status", Value: "published"},
			wantNames: []string{"tpl_value_match_select"}},
		{name: "value match needs column", in: ResolveInput{TemplateID: TemplateIDValueMatch, Roles: roles, Value: "x"}, wantField: "column"},
		{name: "value match needs value", in: ResolveInput{TemplateID: TemplateIDValueMatch, Roles: roles, Column: "status"}, wantField: "value"},
		{name: "open read owner write", in: ResolveInput{TemplateID: TemplateIDOpenReadOwnerWrite, Roles: roles},
			wantNames: []string{"tpl_open_read_owner_write_select", "tpl_open_read_owner_write_update", "tpl_open_read_owner_write_delete"}},
		{name: "blocked by default has no policy", in: ResolveInput{TemplateID: TemplateIDBlockedByDefault}, wantErr: ErrNoPolicyTemplate},
		{name: "unknown template", in: ResolveInput{TemplateID: "nope"}, wantField: "template_id"},
		{name: "empty template", in: ResolveInput{}, wantField: "template_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, err := Resolve(tc.in)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
			case tc.wantField != "":
				var me *MissingInputError
				if !errors.As(err, &me) || me.Field != tc.wantField {
					t.Fatalf("want missing input %q, got %v", tc.wantField, err)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(defs) != len(tc.wantNames) {
					t.Fatalf("got %d policies, want %d: %+v", len(defs), len(tc.wantNames), defs)
				}
				for i, d := range defs {
					if d.Name != tc.wantNames[i] {
						t.Errorf("policy %d = %q, want %q", i, d.Name, tc.wantNames[i])
					}
				}
			}
		})
	}
}
