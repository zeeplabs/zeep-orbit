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

// A lookup that read the token as active before a revoke must not cache it
// after the revoke's Clear/Invalidate ran.
func TestSetIfGeneration_DroppedAfterClearOrInvalidate(t *testing.T) {
	g := Generation()
	Clear()
	SetIfGeneration("jti-raced", true, g)
	if _, fresh := IsActive("jti-raced"); fresh {
		t.Fatal("a fill started before Clear must be dropped")
	}

	g = Generation()
	Invalidate("jti-other")
	SetIfGeneration("jti-raced", true, g)
	if _, fresh := IsActive("jti-raced"); fresh {
		t.Fatal("a fill started before Invalidate must be dropped")
	}

	g = Generation()
	SetIfGeneration("jti-ok", true, g)
	if active, fresh := IsActive("jti-ok"); !active || !fresh {
		t.Fatal("a fill with the current generation should be stored")
	}
}
