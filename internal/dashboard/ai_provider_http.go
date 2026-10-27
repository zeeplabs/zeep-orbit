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
// Limitation: with HTTP(S)_PROXY set in the environment the connection goes to
// the proxy, so this guard sees the proxy's address, not the target's. Proxy
// egress policy is then the operator's.

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
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
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, errAIProviderAddrBlocked
		}
		// Literal spellings first (decimal/hex/octal forms and metadata names
		// included): no lookup is needed to know they are refused.
		if isBlockedAIProviderHost(host) {
			return nil, errAIProviderAddrBlocked
		}
		var ips []netip.Addr
		if ip, perr := netip.ParseAddr(host); perr == nil {
			ips = []netip.Addr{ip}
		} else {
			ips, err = lookup(ctx, host)
			if err != nil {
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
