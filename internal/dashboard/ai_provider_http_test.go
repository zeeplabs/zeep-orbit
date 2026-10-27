package dashboard

// ai_provider_http_test.go — the dial-time SSRF guard of the AI provider HTTP
// client (ai-frontend-builder T30). No real network: hostnames resolve through
// a test lookup, and the only listeners are httptest servers on loopback.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
)

// fixedLookup answers every hostname from a table; unknown names fail.
func fixedLookup(table map[string][]string) aiProviderLookup {
	return func(_ context.Context, host string) ([]netip.Addr, error) {
		raw, ok := table[host]
		if !ok {
			return nil, errors.New("no such host")
		}
		out := make([]netip.Addr, 0, len(raw))
		for _, s := range raw {
			out = append(out, netip.MustParseAddr(s))
		}
		return out, nil
	}
}

// hitServer is a loopback server counting the requests that reach it. If the
// guard failed, a hostname resolving to a blocked IP would never get here, so
// the counter proves "refused before connecting" only for the loopback case;
// the blocked-IP cases below assert on the error instead.
func hitServer(t *testing.T) (srv *httptest.Server, hits *atomic.Int32, port string) {
	t.Helper()
	hits = new(atomic.Int32)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return srv, hits, u.Port()
}

func TestAIProviderHTTPClient_RefusesHostnameResolvingToMetadataIP(t *testing.T) {
	_, hits, port := hitServer(t)

	for name, answers := range map[string][]string{
		"aws/gcp/azure metadata":         {"169.254.169.254"},
		"link-local v6":                  {"fe80::1"},
		"aws imds v6":                    {"fd00:ec2::254"},
		"alibaba metadata":               {"100.100.100.200"},
		"mixed answer hides a bad entry": {"127.0.0.1", "169.254.169.254"},
		"v4-mapped metadata":             {"::ffff:169.254.169.254"},
	} {
		t.Run(name, func(t *testing.T) {
			client := newAIProviderHTTPClient(fixedLookup(map[string][]string{"llm.example.test": answers}))
			resp, err := client.Get("http://llm.example.test:" + port + "/v1/chat/completions")
			if err == nil {
				resp.Body.Close()
				t.Fatal("request to a hostname resolving to a blocked address succeeded")
			}
			if !errors.Is(err, errAIProviderAddrBlocked) {
				t.Fatalf("error = %v, want errAIProviderAddrBlocked", err)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("the listener received %d request(s); a refused dial must not connect", hits.Load())
	}
}

// Private and loopback destinations stay allowed (an Ollama on the internal
// network is a supported setup).
func TestAIProviderHTTPClient_AllowsHostnameResolvingToLoopback(t *testing.T) {
	_, hits, port := hitServer(t)
	client := newAIProviderHTTPClient(fixedLookup(map[string][]string{"ollama.internal.test": {"127.0.0.1"}}))

	resp, err := client.Get("http://ollama.internal.test:" + port + "/")
	if err != nil {
		t.Fatalf("loopback-resolving host must be reachable: %v", err)
	}
	resp.Body.Close()
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
}

// IP literals (also the odd spellings getaddrinfo accepts) are refused without
// any lookup.
func TestAIProviderHTTPClient_RefusesBlockedLiterals(t *testing.T) {
	lookupCalled := false
	client := newAIProviderHTTPClient(func(context.Context, string) ([]netip.Addr, error) {
		lookupCalled = true
		return nil, errors.New("must not be called")
	})
	for _, host := range []string{"169.254.169.254", "2852039166", "0xa9fea9fe", "[fe80::1]", "metadata.google.internal"} {
		resp, err := client.Get("http://" + host + ":80/")
		if err == nil {
			resp.Body.Close()
			t.Fatalf("%s: request succeeded", host)
		}
		if !errors.Is(err, errAIProviderAddrBlocked) {
			t.Errorf("%s: error = %v, want errAIProviderAddrBlocked", host, err)
		}
	}
	if lookupCalled {
		t.Error("a literal blocked address must be refused before any DNS lookup")
	}
}

// A redirect is never followed: it could steer the API key to another host.
func TestAIProviderHTTPClient_DoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redir.Close()

	resp, err := newAIProviderHTTPClient(nil).Get(redir.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || followed.Load() != 0 {
		t.Fatalf("status=%d followed=%d; the redirect must be returned, not followed", resp.StatusCode, followed.Load())
	}
}

// The Control hook is the last line: it refuses on the final ip:port even when
// something else resolved the name.
func TestAIProviderDialControl(t *testing.T) {
	for addr, wantBlocked := range map[string]bool{
		"169.254.169.254:80":      true,
		"[fe80::1]:443":           true,
		"[::ffff:169.254.1.1]:80": true,
		"10.0.0.5:11434":          false,
		"127.0.0.1:8080":          false,
		"203.0.113.10:443":        false,
		"not-an-address":          true, // unparsable: fail closed
	} {
		err := aiProviderDialControl("tcp", addr, nil)
		if blocked := errors.Is(err, errAIProviderAddrBlocked); blocked != wantBlocked {
			t.Errorf("%s: blocked = %v, want %v", addr, blocked, wantBlocked)
		}
	}
}

// End to end through the adapter: the provider's Chat on a base URL whose host
// resolves to the metadata IP fails as a generic provider error and nothing
// of the address or URL is in the error text.
func TestAIProviderHTTPClient_ThroughOpenAIAdapter(t *testing.T) {
	client := newAIProviderHTTPClient(fixedLookup(map[string][]string{"llm.example.test": {"169.254.169.254"}}))
	p, err := provider.NewOpenAI(provider.OpenAIConfig{
		Name: "openai_compatible", BaseURL: "http://llm.example.test:11434/v1", APIKey: "sk-secret", Model: "m", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Chat(context.Background(), provider.ChatRequest{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	var pe *provider.ErrProvider
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want *provider.ErrProvider", err)
	}
	if !errors.Is(err, errAIProviderAddrBlocked) {
		t.Errorf("the cause must be the dial guard, got %v", err)
	}
	if msg := err.Error(); strings.Contains(msg, "169.254") || strings.Contains(msg, "llm.example") || strings.Contains(msg, "sk-secret") {
		t.Errorf("error text leaks the destination or key: %q", msg)
	}
}
