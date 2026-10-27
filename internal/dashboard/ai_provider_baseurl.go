package dashboard

// ai_provider_baseurl.go — validation of the `base_url` of an
// `openai_compatible` AI provider (ai-frontend-builder T21, AFB-01).
//
// The superadmin may point the builder at any OpenAI-compatible endpoint,
// including a private one (an Ollama container on the internal network, so
// plain http and private/loopback hosts are allowed). What must never be
// reachable through this setting are link-local and cloud-metadata endpoints:
// the dashboard (and later the builder worker) would otherwise send the
// provider API key and prompts to the instance metadata service, or let a
// compromised superadmin session read instance credentials through error
// paths.
//
// This is a save-time check on the *literal* host. It cannot defend against a
// hostname that resolves to a blocked address (DNS rebinding included): the
// HTTP client that actually calls the provider must repeat the IP check at
// dial time with isBlockedAIProviderIP (net.Dialer.Control). See the T21
// report.

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const maxAIProviderBaseURLLen = 2048

// Fixed validation messages. They are shown to the superadmin as the 400
// body; none of them echoes the submitted URL (it may contain credentials
// that the user pasted by mistake).
var (
	errBaseURLInvalid     = errors.New("base_url must be an absolute http or https URL")
	errBaseURLTooLong     = errors.New("base_url is too long")
	errBaseURLCredentials = errors.New("base_url must not contain credentials")
	errBaseURLQuery       = errors.New("base_url must not contain a query string or fragment")
	errBaseURLBlocked     = errors.New("base_url points to a link-local or cloud metadata address, which is not allowed")
)

// blockedMetadataHostnames are DNS names of cloud metadata services. They are
// matched exactly (case-insensitive, trailing dot ignored).
var blockedMetadataHostnames = map[string]bool{
	"metadata.google.internal":     true,
	"metadata":                     true,
	"instance-data":                true,
	"instance-data.ec2.internal":   true,
	"metadata.tencentyun.com":      true,
	"metadata.platformequinix.com": true,
}

var (
	awsIMDSv6      = netip.MustParseAddr("fd00:ec2::254")
	alibabaIMDS    = netip.MustParseAddr("100.100.100.200")
	azureWireserve = netip.MustParseAddr("168.63.129.16")
	nat64Prefix    = netip.MustParsePrefix("64:ff9b::/96")
	v4CompatPrefix = netip.MustParsePrefix("::/96")
)

// normalizeAIProviderBaseURL validates raw and returns the canonical form to
// store (trimmed, no trailing slash). An empty (after trim) input returns
// ("", nil): the caller decides whether empty is acceptable.
func normalizeAIProviderBaseURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if len(s) > maxAIProviderBaseURLLen {
		return "", errBaseURLTooLong
	}
	for _, r := range s {
		// Control characters and spaces have no place in a base URL and are
		// a classic way to smuggle a second host past a naive check.
		if r <= 0x20 || r == 0x7f {
			return "", errBaseURLInvalid
		}
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", errBaseURLInvalid
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errBaseURLInvalid
	}
	if u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return "", errBaseURLInvalid
	}
	if u.User != nil {
		return "", errBaseURLCredentials
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(s, "#") {
		return "", errBaseURLQuery
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", errBaseURLInvalid
		}
	}
	if isBlockedAIProviderHost(u.Hostname()) {
		return "", errBaseURLBlocked
	}
	return strings.TrimRight(s, "/"), nil
}

// isBlockedAIProviderHost reports whether host (as returned by
// url.URL.Hostname: no brackets, no port) is a blocked metadata hostname or a
// literal IP in any spelling the system resolver would accept that lands on a
// blocked address.
func isBlockedAIProviderHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if blockedMetadataHostnames[h] {
		return true
	}
	// IPv6 zone ("fe80::1%eth0"): the address part decides.
	if i := strings.IndexByte(h, '%'); i >= 0 {
		h = h[:i]
	}
	if addr, err := netip.ParseAddr(h); err == nil {
		return isBlockedAIProviderIP(addr)
	}
	if addr, ok := parseLooseIPv4(h); ok {
		return isBlockedAIProviderIP(addr)
	}
	return false
}

// isBlockedAIProviderIP reports whether addr is link-local (169.254.0.0/16,
// fe80::/10), multicast, or a known cloud metadata address. Private and
// loopback addresses are intentionally allowed. IPv4-mapped (::ffff:a.b.c.d),
// IPv4-compatible (::a.b.c.d) and NAT64 (64:ff9b::a.b.c.d) IPv6 forms are
// judged by the IPv4 address they embed. It is also the predicate a dial-time
// check should use.
func isBlockedAIProviderIP(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	if addr.Is6() {
		b := addr.As16()
		switch {
		case nat64Prefix.Contains(addr):
			addr = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
		case v4CompatPrefix.Contains(addr) && !addr.IsUnspecified() && addr != netip.IPv6Loopback():
			addr = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
		}
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() || addr.IsMulticast() {
		return true
	}
	return addr == awsIMDSv6 || addr == alibabaIMDS || addr == azureWireserve
}

// parseLooseIPv4 parses the IPv4 spellings accepted by inet_aton(3), which
// getaddrinfo (and therefore the cgo resolver, curl, browsers) treats as an
// IP literal but Go's netip.ParseAddr rejects: a single 32-bit number
// ("2852039166"), hexadecimal ("0xa9fea9fe", "0xA9.0xFE.0xA9.0xFE"), octal
// ("0251.0376.0251.0376") and the 2- and 3-part short forms
// ("169.254.43518", "169.16690686"). Each part may independently be decimal,
// 0x-hex or 0-octal; the last part fills all remaining bytes.
func parseLooseIPv4(host string) (netip.Addr, bool) {
	parts := strings.Split(host, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return netip.Addr{}, false
	}
	vals := make([]uint64, len(parts))
	for i, p := range parts {
		v, ok := parseIPv4Part(p)
		if !ok {
			return netip.Addr{}, false
		}
		vals[i] = v
	}
	var n uint64
	for i := 0; i < len(vals)-1; i++ {
		if vals[i] > 255 {
			return netip.Addr{}, false
		}
		n |= vals[i] << (8 * uint(3-i))
	}
	last := vals[len(vals)-1]
	// The last part holds 8*(5-len(parts)) bits.
	if last >= uint64(1)<<(8*uint(5-len(parts))) {
		return netip.Addr{}, false
	}
	n |= last
	return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}), true
}

func parseIPv4Part(p string) (uint64, bool) {
	if p == "" {
		return 0, false
	}
	base := 10
	digits := p
	switch {
	case len(p) > 2 && (p[:2] == "0x" || p[:2] == "0X"):
		base, digits = 16, p[2:]
	case len(p) > 1 && p[0] == '0':
		base, digits = 8, p[1:]
	}
	v, err := strconv.ParseUint(digits, base, 32)
	if err != nil {
		return 0, false
	}
	return v, true
}
