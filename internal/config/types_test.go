package config

import "testing"

func TestReferenceConfig_Equal_BothNil(t *testing.T) {
	var a, b *ReferenceConfig
	if !a.Equal(b) {
		t.Fatalf("expected both-nil references to be equal")
	}
}

func TestReferenceConfig_Equal_Identical(t *testing.T) {
	a := &ReferenceConfig{Table: "customers", Column: "id", OnDelete: "cascade"}
	b := &ReferenceConfig{Table: "customers", Column: "id", OnDelete: "cascade"}
	if !a.Equal(b) {
		t.Fatalf("expected identical references to be equal")
	}
}

func TestReferenceConfig_Equal_DifferingField(t *testing.T) {
	base := &ReferenceConfig{Table: "customers", Column: "id", OnDelete: "cascade"}

	cases := []struct {
		name  string
		other *ReferenceConfig
	}{
		{"table differs", &ReferenceConfig{Table: "orders", Column: "id", OnDelete: "cascade"}},
		{"column differs", &ReferenceConfig{Table: "customers", Column: "uuid", OnDelete: "cascade"}},
		{"on_delete differs", &ReferenceConfig{Table: "customers", Column: "id", OnDelete: "restrict"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if base.Equal(tc.other) {
				t.Fatalf("expected references differing on %s to not be equal", tc.name)
			}
		})
	}
}

func TestReferenceConfig_Equal_ExactlyOneNil(t *testing.T) {
	a := &ReferenceConfig{Table: "customers", Column: "id"}
	var b *ReferenceConfig

	if a.Equal(b) {
		t.Fatalf("expected non-nil vs nil to not be equal")
	}
	if b.Equal(a) {
		t.Fatalf("expected nil vs non-nil to not be equal")
	}
}
