package tokencache

import "testing"

func TestClear(t *testing.T) {
	Set("jti-a", true)
	Set("jti-b", false)
	if _, fresh := IsActive("jti-a"); !fresh {
		t.Fatal("jti-a should be cached before Clear")
	}

	Clear()

	for _, jti := range []string{"jti-a", "jti-b"} {
		if _, fresh := IsActive(jti); fresh {
			t.Fatalf("%s should not be cached after Clear", jti)
		}
	}

	Set("jti-c", true)
	if active, fresh := IsActive("jti-c"); !active || !fresh {
		t.Fatal("Set after Clear should work")
	}
}
