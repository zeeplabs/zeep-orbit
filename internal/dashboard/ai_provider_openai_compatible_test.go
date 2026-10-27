package dashboard

// ai_provider_openai_compatible_test.go — coverage for the `openai_compatible`
// provider and its base_url (ai-frontend-builder T21, AFB-01). The handler
// tests run against a real Postgres (TEST_DATABASE_URL) through
// aiProviderHandlerTestPool; the validation tests are pure.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

const ocProvider = "openai_compatible"

func putOpenAICompatible(t *testing.T, h *Handler, user *DashboardUser, provider, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := requestWithProvider(http.MethodPut, "/dashboard/api/ai-providers/"+provider, body, user, provider)
	w := httptest.NewRecorder()
	h.UpsertAIProviderConfig(w, req)
	return w
}

func getAIProviderHTTP(t *testing.T, h *Handler, user *DashboardUser, provider string) (*httptest.ResponseRecorder, AIProviderResponse) {
	t.Helper()
	req := requestWithProvider(http.MethodGet, "/dashboard/api/ai-providers/"+provider, "", user, provider)
	w := httptest.NewRecorder()
	h.GetAIProviderConfig(w, req)
	var got AIProviderResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal GET: %v", err)
		}
	}
	return w, got
}

// Save with base_url: persisted (normalized), has_key reflects the key, and
// neither the PUT nor the GET body ever carries the key.
func TestOpenAICompatible_SaveWithBaseURLAndGetNeverReturnsKey(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := putOpenAICompatible(t, h, super, ocProvider,
		`{"model":"llama3.1","api_key":"sk-compat-secret-123","base_url":" https://llm.internal.example/v1/ ","enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sk-compat-secret-123") {
		t.Fatal("PUT response must never echo the key")
	}

	gw, got := getAIProviderHTTP(t, h, super, ocProvider)
	if gw.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", gw.Code)
	}
	if got.BaseURL != "https://llm.internal.example/v1" {
		t.Errorf("base_url = %q, want trimmed and without trailing slash", got.BaseURL)
	}
	if !got.HasKey || got.Model != "llama3.1" || !got.Enabled || got.Provider != ocProvider {
		t.Errorf("unexpected GET body: %+v", got)
	}
	if strings.Contains(gw.Body.String(), "sk-compat-secret-123") || strings.Contains(gw.Body.String(), "api_key") {
		t.Fatalf("GET must never carry key material: %s", gw.Body.String())
	}
}

// A model-only update (no api_key, no base_url) keeps both stored values.
func TestOpenAICompatible_ModelOnlyUpdateKeepsKeyAndBaseURL(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	if w := putOpenAICompatible(t, h, super, ocProvider,
		`{"model":"m1","api_key":"sk-keep-me","base_url":"http://10.0.0.5:11434/v1","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("initial PUT: %d %s", w.Code, w.Body.String())
	}
	if w := putOpenAICompatible(t, h, super, ocProvider, `{"model":"m2","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("model-only PUT: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	rt, err := ResolveAIProviderRuntime(context.Background(), pool, ocProvider)
	if err != nil {
		t.Fatalf("ResolveAIProviderRuntime: %v", err)
	}
	if rt.Model != "m2" || rt.APIKey != "sk-keep-me" || rt.BaseURL != "http://10.0.0.5:11434/v1" {
		t.Fatalf("model-only update must keep key and base_url, got model=%q baseURL=%q keyKept=%v", rt.Model, rt.BaseURL, rt.APIKey == "sk-keep-me")
	}
}

// An explicit new base_url replaces the stored one while the key is kept.
func TestOpenAICompatible_BaseURLChangeKeepsKey(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	putOpenAICompatible(t, h, super, ocProvider, `{"model":"m","api_key":"sk-stay","base_url":"https://a.example/v1","enabled":true}`)
	if w := putOpenAICompatible(t, h, super, ocProvider, `{"model":"m","base_url":"https://b.example/v1","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	rt, err := ResolveAIProviderRuntime(context.Background(), pool, ocProvider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rt.BaseURL != "https://b.example/v1" || rt.APIKey != "sk-stay" {
		t.Fatalf("unexpected runtime: baseURL=%q keyKept=%v", rt.BaseURL, rt.APIKey == "sk-stay")
	}
}

// base_url is required: not on first save, and cannot be cleared afterward.
func TestOpenAICompatible_BaseURLRequired(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	for name, body := range map[string]string{
		"absent on first save":  `{"model":"m","enabled":true}`,
		"blank on first save":   `{"model":"m","base_url":"   ","enabled":true}`,
		"explicit empty string": `{"model":"m","base_url":"","enabled":true}`,
	} {
		w := putOpenAICompatible(t, h, super, ocProvider, body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "base_url is required") {
			t.Errorf("%s: expected 400 base_url required, got %d %s", name, w.Code, w.Body.String())
		}
	}
	if got, _ := GetAIProvider(context.Background(), pool, ocProvider); got.Model != "" || got.BaseURL != "" {
		t.Fatalf("rejected PUTs must not persist anything, got %+v", got)
	}

	// Once configured, an explicit "" must not clear the base URL.
	putOpenAICompatible(t, h, super, ocProvider, `{"model":"m","base_url":"https://ok.example/v1","enabled":true}`)
	if w := putOpenAICompatible(t, h, super, ocProvider, `{"model":"m","base_url":"","enabled":true}`); w.Code != http.StatusBadRequest {
		t.Fatalf("clearing base_url: expected 400, got %d", w.Code)
	}
	if got, _ := GetAIProvider(context.Background(), pool, ocProvider); got.BaseURL != "https://ok.example/v1" {
		t.Fatalf("base_url must be unchanged after a rejected clear, got %q", got.BaseURL)
	}
}

// The key is optional for openai_compatible (e.g. Ollama): the row resolves
// with an empty key and the base URL.
func TestOpenAICompatible_KeylessProviderResolves(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	if w := putOpenAICompatible(t, h, super, ocProvider,
		`{"model":"qwen2.5-coder","base_url":"http://ollama:11434/v1","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	_, got := getAIProviderHTTP(t, h, super, ocProvider)
	if got.HasKey {
		t.Error("has_key must be false when no key was sent")
	}
	rt, err := ResolveAIProviderRuntime(context.Background(), pool, ocProvider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rt.APIKey != "" || rt.BaseURL != "http://ollama:11434/v1" || rt.Model != "qwen2.5-coder" || rt.Provider != ocProvider {
		t.Fatalf("unexpected runtime: %+v", rt)
	}
}

// Anything that is not an absolute http(s) URL, or that carries credentials,
// a query or a fragment, is rejected with 400 and nothing is persisted.
func TestOpenAICompatible_InvalidBaseURLRejected(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	bad := []string{
		"ftp://example.com/v1",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"gopher://example.com",
		"example.com/v1",
		"//example.com/v1",
		"localhost:11434/v1",
		"http://",
		"https:///v1",
		"/v1",
		"http://user:pass@example.com/v1",
		"http://user@example.com/v1",
		"https://example.com/v1?key=1",
		"https://example.com/v1#frag",
		"https://exa mple.com/v1",
		"https://example.com:99999/v1",
		"https://" + strings.Repeat("a", 2100) + ".example/v1",
	}
	for _, u := range bad {
		body, _ := json.Marshal(map[string]any{"model": "m", "base_url": u, "enabled": true})
		w := putOpenAICompatible(t, h, super, ocProvider, string(body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("base_url %q: expected 400, got %d: %s", u, w.Code, w.Body.String())
			continue
		}
		if strings.Contains(w.Body.String(), "pass") || strings.Contains(w.Body.String(), "user@") {
			t.Errorf("base_url %q: the error must not echo the submitted URL: %s", u, w.Body.String())
		}
	}
	if got, _ := GetAIProvider(context.Background(), pool, ocProvider); got.BaseURL != "" || got.Model != "" {
		t.Fatalf("no rejected PUT may persist anything, got %+v", got)
	}
}

// Link-local and cloud-metadata addresses are rejected in every literal
// spelling the system resolver would accept; private and loopback hosts are
// allowed (the setting is superadmin-only and Ollama lives on a private net).
func TestOpenAICompatible_MetadataAddressesRejectedPrivateAllowed(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	blocked := []string{
		"http://169.254.169.254/latest/meta-data",
		"http://169.254.169.254:80/v1",
		"https://169.254.0.1/v1",
		"http://2852039166/",                       // decimal
		"http://0xa9fea9fe/",                       // hex
		"http://0xA9.0xFE.0xA9.0xFE/",              // hex per octet
		"http://0251.0376.0251.0376/",              // octal per octet
		"http://169.254.43518/",                    // 3-part form
		"http://169.16690686/",                     // 2-part form
		"http://[::ffff:169.254.169.254]/",         // IPv4-mapped IPv6, dotted
		"http://[::ffff:a9fe:a9fe]/",               // IPv4-mapped IPv6, hex
		"http://[0:0:0:0:0:ffff:a9fe:a9fe]/v1",     // IPv4-mapped IPv6, expanded
		"http://[fd00:ec2::254]/",                  // AWS IMDS over IPv6
		"http://[fd00:ec2:0:0:0:0:0:254]/",         // same, expanded
		"http://[FD00:EC2::254]:8080/",             // same, upper case + port
		"http://[fe80::1]/",                        // IPv6 link-local
		"http://[fe80::1%25eth0]/",                 // IPv6 link-local with zone
		"http://[64:ff9b::a9fe:a9fe]/",             // NAT64 of 169.254.169.254
		"http://[::a9fe:a9fe]/",                    // IPv4-compatible
		"http://100.100.100.200/",                  // Alibaba Cloud metadata
		"http://168.63.129.16/",                    // Azure wireserver
		"http://metadata.google.internal/v1",       // GCP metadata hostname
		"http://Metadata.Google.Internal./v1",      // case + trailing dot
		"http://169.254.169.254./",                 // trailing dot
		"https://169.254.169.254/v1",               // scheme does not matter
		"HTTP://169.254.169.254/",                  // scheme case
		"http://0xa9.254.169.254/",                 // mixed bases
		"http://0251.254.169.254/v1",               // octal first octet
		"http://224.0.0.251/",                      // multicast
		"http://[ff02::fb]/",                       // IPv6 multicast
		"http://instance-data.ec2.internal/latest", // AWS alias
	}
	for _, u := range blocked {
		body, _ := json.Marshal(map[string]any{"model": "m", "base_url": u, "enabled": true})
		w := putOpenAICompatible(t, h, super, ocProvider, string(body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("base_url %q: expected 400, got %d: %s", u, w.Code, w.Body.String())
		}
	}
	if got, _ := GetAIProvider(context.Background(), pool, ocProvider); got.BaseURL != "" {
		t.Fatalf("a blocked address must never be persisted, got %q", got.BaseURL)
	}

	allowed := []string{
		"http://10.0.0.5:11434/v1",
		"http://172.16.4.2/v1",
		"http://192.168.1.10:8000/v1",
		"http://localhost:11434/v1",
		"http://127.0.0.1:11434/v1",
		"http://[::1]:11434/v1",
		"http://ollama:11434/v1",
		"http://host.docker.internal:11434/v1",
		"https://api.openai.com/v1",
		"https://generativelanguage.googleapis.com/v1beta/openai",
		"http://169.253.1.1/v1", // just outside 169.254.0.0/16
	}
	for _, u := range allowed {
		body, _ := json.Marshal(map[string]any{"model": "m", "base_url": u, "enabled": true})
		w := putOpenAICompatible(t, h, super, ocProvider, string(body))
		if w.Code != http.StatusOK {
			t.Errorf("base_url %q: expected 200, got %d: %s", u, w.Code, w.Body.String())
		}
	}
}

// Only a superadmin may write; the 403 happens before anything is stored.
func TestOpenAICompatible_NonSuperadminForbidden(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	regular := mustCreateRegularUser(t, pool)

	w := putOpenAICompatible(t, h, regular, ocProvider,
		`{"model":"m","api_key":"sk-no","base_url":"https://llm.example/v1","enabled":true}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	got, err := GetAIProvider(context.Background(), pool, ocProvider)
	if err != nil {
		t.Fatalf("GetAIProvider: %v", err)
	}
	if got.HasKey || got.BaseURL != "" || got.Model != "" {
		t.Fatalf("forbidden PUT must not mutate anything, got %+v", got)
	}
}

// openai stays as it was: it accepts no base URL (400) but an explicit empty
// one is a harmless no-op, and the existing behavior without the field is
// unchanged (covered by the pre-existing tests).
func TestOpenAI_RejectsBaseURLButBehaviorUnchanged(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	if w := putOpenAICompatible(t, h, super, "openai",
		`{"model":"gpt-4o","api_key":"sk-x","base_url":"https://evil.example/v1","enabled":true}`); w.Code != http.StatusBadRequest {
		t.Fatalf("openai + base_url: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if w := putOpenAICompatible(t, h, super, "openai",
		`{"model":"gpt-4o","api_key":"sk-x","base_url":"","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("openai + empty base_url: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	_, got := getAIProviderHTTP(t, h, super, "openai")
	if got.BaseURL != "" || !got.HasKey || got.Model != "gpt-4o" {
		t.Fatalf("unexpected openai config: %+v", got)
	}
	// The legacy accessor used by the chat handlers still works.
	if _, key, err := resolveDecryptedAIProviderKey(context.Background(), pool, "openai"); err != nil || key != "sk-x" {
		t.Fatalf("legacy resolveDecryptedAIProviderKey: key ok=%v err=%v", key == "sk-x", err)
	}
}

// A successful save writes an audit row for the provider, without the key.
func TestOpenAICompatible_AuditRowWithoutKey(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	putOpenAICompatible(t, h, super, ocProvider,
		`{"model":"m","api_key":"sk-audit-secret","base_url":"https://llm.example/v1","enabled":true}`)

	entries, _, err := ListAuditLog(context.Background(), pool, AuditLogFilter{Action: "ai_provider.update"})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if len(entries) != 1 || entries[0].ResourceID != ocProvider {
		t.Fatalf("expected one ai_provider.update row for %s, got %+v", ocProvider, entries)
	}
	if strings.Contains(string(entries[0].Metadata), "sk-audit-secret") {
		t.Fatal("audit metadata must never contain the key")
	}
	if !strings.Contains(string(entries[0].Metadata), "https://llm.example/v1") {
		t.Errorf("audit metadata should record the base_url, got %s", entries[0].Metadata)
	}
}

// ResolveAIProviderRuntime treats missing, disabled, key-less openai and
// unsupported providers as unavailable.
func TestResolveAIProviderRuntime_Unavailable(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)
	ctx := context.Background()

	if _, err := ResolveAIProviderRuntime(ctx, pool, ocProvider); !errors.Is(err, ErrAIProviderUnavailable) {
		t.Errorf("missing row: expected ErrAIProviderUnavailable, got %v", err)
	}
	if _, err := ResolveAIProviderRuntime(ctx, pool, "gemini"); !errors.Is(err, ErrAIProviderUnavailable) {
		t.Errorf("gemini: expected ErrAIProviderUnavailable, got %v", err)
	}

	putOpenAICompatible(t, h, super, ocProvider, `{"model":"m","base_url":"https://x.example/v1","enabled":false}`)
	if _, err := ResolveAIProviderRuntime(ctx, pool, ocProvider); !errors.Is(err, ErrAIProviderUnavailable) {
		t.Errorf("disabled: expected ErrAIProviderUnavailable, got %v", err)
	}

	putOpenAICompatible(t, h, super, "openai", `{"model":"gpt-4o","enabled":true}`)
	if _, err := ResolveAIProviderRuntime(ctx, pool, "openai"); !errors.Is(err, ErrAIProviderUnavailable) {
		t.Errorf("openai without key: expected ErrAIProviderUnavailable, got %v", err)
	}
	putOpenAICompatible(t, h, super, "openai", `{"model":"gpt-4o","api_key":"sk-real","enabled":true}`)
	rt, err := ResolveAIProviderRuntime(ctx, pool, "openai")
	if err != nil || rt.APIKey != "sk-real" || rt.BaseURL != "" {
		t.Errorf("openai with key: rt=%+v err=%v", rt, err)
	}
}

// Pure unit test of the address predicate, independent of the HTTP layer.
func TestIsBlockedAIProviderHost(t *testing.T) {
	cases := map[string]bool{
		"169.254.169.254":          true,
		"2852039166":               true,
		"0xa9fea9fe":               true,
		"0251.0376.0251.0376":      true,
		"::ffff:169.254.169.254":   true,
		"::ffff:a9fe:a9fe":         true,
		"fd00:ec2::254":            true,
		"fe80::abcd":               true,
		"fe80::1%eth0":             true,
		"64:ff9b::a9fe:a9fe":       true,
		"metadata.google.internal": true,
		"10.1.2.3":                 false,
		"127.0.0.1":                false,
		"::1":                      false,
		"fd00:ec2::255":            false,
		"fd12::254":                false,
		"2130706433":               false, // 127.0.0.1 as a decimal: loopback is allowed
		"ollama":                   false,
		"example.com":              false,
		"1.2.3.4.5":                false, // not an IP in any spelling: a hostname
		"999.1.1.1":                false,
		"08.1.1.1":                 false, // invalid octal digit: not an IP literal
	}
	for host, want := range cases {
		if got := isBlockedAIProviderHost(host); got != want {
			t.Errorf("isBlockedAIProviderHost(%q) = %v, want %v", host, got, want)
		}
	}

	if !isBlockedAIProviderIP(netip.MustParseAddr("169.254.1.1")) {
		t.Error("169.254.1.1 must be blocked")
	}
	if isBlockedAIProviderIP(netip.MustParseAddr("169.255.1.1")) {
		t.Error("169.255.1.1 must not be blocked")
	}
}

// normalizeAIProviderBaseURL canonicalizes without altering host or path.
func TestNormalizeAIProviderBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                                 "",
		"   ":                              "",
		"https://api.example.com/v1/":      "https://api.example.com/v1",
		"  http://localhost:11434/v1  ":    "http://localhost:11434/v1",
		"https://api.example.com///":       "https://api.example.com",
		"https://api.example.com/v1/chat/": "https://api.example.com/v1/chat",
	}
	for in, want := range cases {
		got, err := normalizeAIProviderBaseURL(in)
		if err != nil || got != want {
			t.Errorf("normalizeAIProviderBaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
