package dashboard

// ai_provider_http.go — the HTTP client the AI Frontend Builder uses to call
// the configured LLM provider (ai-frontend-builder T30, AFB-01).
//
// normalizeAIProviderBaseURL (ai_provider_baseurl.go) only judges the literal
// spelling of the host when the superadmin saves the provider. A hostname that
// RESOLVES to a link-local or cloud-metadata address (or one whose DNS answer
// changes after the save: DNS rebinding) passes that check. This file repeats
// the check where it cannot be bypassed, at connect time, on the IP the
// connection actually goes to:
//
//   - DialContext resolves the host itself, rejects the dial when ANY answer is
//     blocked (a mixed answer is how a rebinding attack hides the bad address),
//     and then connects to the vetted IP literal, so the address that was
//     checked is the address that is dialed.
//   - net.Dialer.Control re-checks the final address the OS is about to connect
//     to (defense in depth: covers IP-literal URLs and redirects).
//
// Private and loopback addresses stay allowed (an Ollama container on the
// internal network is a supported setup); isBlockedAIProviderIP decides.
//
// With HTTP(S)_PROXY set in the environment the connection goes to the proxy,
// so DialContext sees the proxy's address, not the target's. The Proxy hook
// therefore vets the target host itself before a proxy is chosen. What the
// proxy then resolves is the operator's egress policy (a rebinding answer given
// only to the proxy is not visible here).

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

const (
	aiProviderHTTPTimeout = 120 * time.Second // same bound as the provider adapter's default
	aiProviderDialTimeout = 10 * time.Second
)

// errAIProviderAddrBlocked is what a refused dial fails with. It carries no
// host or address: the error text ends up in server-side logs only, but there
// is nothing in it worth leaking.
var errAIProviderAddrBlocked = errors.New("ai provider: destination address is not allowed")

// aiProviderLookup resolves a hostname to its IP addresses. Injected in tests.
type aiProviderLookup func(ctx context.Context, host string) ([]netip.Addr, error)

func defaultAIProviderLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// aiProviderDialControl is a net.Dialer.Control that refuses blocked addresses.
func aiProviderDialControl(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return errAIProviderAddrBlocked // unparsable: fail closed
	}
	if isBlockedAIProviderIP(ap.Addr()) {
		return errAIProviderAddrBlocked
	}
	return nil
}

// vetAIProviderHost resolves host (a name or an IP literal) and returns the
// addresses it may be dialed on. It refuses a blocked literal spelling and any
// answer that contains a blocked address.
func vetAIProviderHost(ctx context.Context, lookup aiProviderLookup, host string) ([]netip.Addr, error) {
	// Literal spellings first (decimal/hex/octal forms and metadata names
	// included): no lookup is needed to know they are refused.
	if isBlockedAIProviderHost(host) {
		return nil, errAIProviderAddrBlocked
	}
	var ips []netip.Addr
	if ip, perr := netip.ParseAddr(host); perr == nil {
		ips = []netip.Addr{ip}
	} else {
		var err error
		if ips, err = lookup(ctx, host); err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, errAIProviderAddrBlocked
	}
	for _, ip := range ips {
		if isBlockedAIProviderIP(ip) {
			return nil, errAIProviderAddrBlocked
		}
	}
	return ips, nil
}

// aiProviderProxy picks the proxy for a request (the environment's, by default).
// A variable so a test can set one: http.ProxyFromEnvironment reads the
// environment once per process.
var aiProviderProxy = http.ProxyFromEnvironment

// newAIProviderHTTPClient returns the *http.Client to pass as
// provider.OpenAIConfig.HTTPClient. lookup nil means the system resolver.
// Redirects are not followed: a chat-completions endpoint has no reason to
// redirect, and a redirect is a way to steer the API key somewhere else.
func newAIProviderHTTPClient(lookup aiProviderLookup) *http.Client {
	if lookup == nil {
		lookup = defaultAIProviderLookup
	}
	dialer := &net.Dialer{
		Timeout:   aiProviderDialTimeout,
		KeepAlive: 30 * time.Second,
		Control:   aiProviderDialControl,
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// With HTTP(S)_PROXY set the connection goes to the proxy, so DialContext
	// only sees the proxy's address. Vet the target here, before a proxy is
	// chosen, so a metadata or link-local target is refused either way.
	proxyFromEnv := aiProviderProxy
	tr.Proxy = func(req *http.Request) (*url.URL, error) {
		proxyURL, err := proxyFromEnv(req)
		if err != nil || proxyURL == nil {
			return proxyURL, err
		}
		if _, err := vetAIProviderHost(req.Context(), lookup, req.URL.Hostname()); err != nil {
			return nil, err
		}
		return proxyURL, nil
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, errAIProviderAddrBlocked
		}
		ips, err := vetAIProviderHost(ctx, lookup, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if derr == nil {
				return conn, nil
			}
			lastErr = derr
			if ctx.Err() != nil {
				break
			}
		}
		return nil, lastErr
	}
	return &http.Client{
		Transport: tr,
		Timeout:   aiProviderHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
