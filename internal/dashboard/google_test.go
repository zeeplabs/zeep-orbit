package dashboard

// google_test.go — unit coverage for normalizeDashboardReturnPath, the
// server-side twin of the frontend's safeReturnTo (src/lib/returnTo.ts).
// Guards against the exact class of bug a prior review caught: a
// return_to value that already carries the "/dashboard" basename must be
// stripped here too, or Callback's "/dashboard"+returnTo concatenation
// produces a broken "/dashboard/dashboard/..." path.

import "testing"

func TestNormalizeDashboardReturnPath(t *testing.T) {
	cases := []struct {
		path     string
		wantOK   bool
		wantPath string
	}{
		{"", false, ""},
		{"/oauth/consent?client_id=abc", true, "/oauth/consent?client_id=abc"},
		{"/dashboard/oauth/authorize?client_id=abc", true, "/oauth/authorize?client_id=abc"},
		{"/dashboard", true, "/"},
		{"/dashboard/", true, "/"},
		{"/dashboard?foo=bar", false, ""},
		{"/dashboardish/path", true, "/dashboardish/path"},
		{"//evil.com", false, ""},
		{`/\evil.com`, false, ""},
		{"https://evil.com", false, ""},
		{"relative/path", false, ""},
	}
	for _, c := range cases {
		got, ok := normalizeDashboardReturnPath(c.path)
		if ok != c.wantOK {
			t.Errorf("normalizeDashboardReturnPath(%q) ok = %v, want %v", c.path, ok, c.wantOK)
			continue
		}
		if ok && got != c.wantPath {
			t.Errorf("normalizeDashboardReturnPath(%q) = %q, want %q", c.path, got, c.wantPath)
		}
	}
}
