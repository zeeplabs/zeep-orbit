package skills

import (
	"strings"
	"testing"
)

// Fake secrets are assembled at run time so the source file never contains a
// literal that secret scanners flag.
func fake(parts ...string) string { return strings.Join(parts, "") }

func TestDetectCredentialsPositives(t *testing.T) {
	alnum := strings.Repeat("a1B2", 12)
	cases := map[string]string{
		"pem rsa":        fake("-----BEGIN RSA ", "PRIVATE KEY-----\nabc"),
		"pem plain":      fake("-----BEGIN ", "PRIVATE KEY-----"),
		"pem openssh":    fake("-----BEGIN OPENSSH ", "PRIVATE KEY-----"),
		"openai":         fake("key = sk", "-", alnum),
		"openai project": fake("sk", "-proj-", strings.Repeat("a_b-", 12)),
		"anthropic":      fake("sk", "-ant-", strings.Repeat("a_b-", 12)),
		"aws":            fake("AK", "IA", "ABCDEFGHIJKLMNOP"),
		"aws temp":       fake("AS", "IA", "ABCDEFGHIJKLMNOP"),
		"github ghp":     fake("gh", "p_", strings.Repeat("a1", 20)),
		"github pat":     fake("github", "_pat_", strings.Repeat("A1_", 10)),
		"slack":          fake("xo", "xb-", "1234567890-abcdef"),
		"google":         fake("AI", "za", strings.Repeat("a1_-", 9)[:35]),
		"jwt":            fake("ey", "Jhbgcioijiuzi1nir", ".", "ey", "Jzdwiioi4mjm0nta", ".", "abcdef_ghijkl-mnop"),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if !DetectCredentials("prefix text\n" + in + "\nsuffix") {
				t.Fatalf("not detected: %s", name)
			}
		})
	}
}

func TestDetectCredentialsNegatives(t *testing.T) {
	cases := map[string]string{
		"prose sk word":   "Use sk-learn-style-guides-for-frontend-work when routing",
		"task prefix":     "task-" + strings.Repeat("a1B2", 12),
		"short sk":        "sk-abc123",
		"short jwt like":  "eyJabc.eyJdef.ghi",
		"short akia":      "AKIA1234",
		"akia lowercase":  "akiaabcdefghijklmnop",
		"plain begin":     "-----BEGIN CERTIFICATE-----",
		"public key":      "-----BEGIN PUBLIC KEY-----",
		"short ghp":       "ghp_abc",
		"normal markdown": "# Title\n\nUse `getOrbit()` to fetch rows.\n\n- item",
		"email":           "contact: someone@example.com",
		"slack channel":   "post in #general or xox",
		"aiza too short":  "AIzaShort",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if DetectCredentials(in) {
				t.Fatalf("false positive: %q", name)
			}
		})
	}
}
