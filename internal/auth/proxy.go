package auth

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies is the set of peer addresses whose X-Forwarded-* headers
// binbash believes.
//
// Those headers are trivially forged: any client can claim
// "X-Forwarded-For: 1.2.3.4" and, if we took that at face value, sail past the
// login throttle with a fresh IP on every attempt. So they are only honoured
// when the request's *actual* peer address (RemoteAddr, which cannot be forged
// on a TCP connection) is one we've been told is a reverse proxy.
//
// The default is loopback only, which is safe precisely because it isn't
// forgeable from off-box: to have RemoteAddr be 127.0.0.1 you must already be
// on the machine. It also means the documented VPS setup -- a TLS-terminating
// proxy on the same host, binbash bound to 127.0.0.1 -- gets correct client
// IPs and Secure cookies with no configuration at all. Deployments where the
// proxy is *not* on loopback (Docker, a proxy on another host) must list it in
// trusted_proxies, or throttling collapses into one shared bucket.
type TrustedProxies struct {
	prefixes []netip.Prefix
}

// DefaultTrustedProxies trusts the loopback addresses and nothing else.
func DefaultTrustedProxies() *TrustedProxies {
	tp, err := ParseTrustedProxies([]string{"127.0.0.1", "::1"})
	if err != nil {
		panic("auth: default trusted proxies must parse: " + err.Error())
	}
	return tp
}

// ParseTrustedProxies builds a TrustedProxies from a list of bare IP addresses
// ("127.0.0.1") and CIDR ranges ("172.17.0.0/16"). An empty list trusts
// nothing, which disables X-Forwarded-* handling entirely.
func ParseTrustedProxies(specs []string) (*TrustedProxies, error) {
	tp := &TrustedProxies{}
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}

		if strings.Contains(spec, "/") {
			prefix, err := netip.ParsePrefix(spec)
			if err != nil {
				return nil, fmt.Errorf("%q is not a valid IP range: %w", spec, err)
			}
			tp.prefixes = append(tp.prefixes, prefix.Masked())
			continue
		}

		addr, err := netip.ParseAddr(spec)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid IP address or range: %w", spec, err)
		}
		addr = addr.Unmap()
		tp.prefixes = append(tp.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return tp, nil
}

// trusts reports whether addr is one of the configured reverse proxies.
func (tp *TrustedProxies) trusts(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range tp.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// peer returns the address the TCP connection actually came from.
func peer(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// ClientIP returns the address to attribute the request to: the peer address,
// or -- when the peer is a trusted proxy -- the real client it is forwarding
// for. This is what the login throttle keys on and what the auth failure log
// reports, so getting it wrong in either direction is a security bug: too
// trusting lets an attacker forge a new identity per attempt, too suspicious
// lumps every remote user into the proxy's single bucket, where five wrong
// guesses lock out everyone.
func (tp *TrustedProxies) ClientIP(r *http.Request) string {
	addr, ok := peer(r)
	if !ok {
		return r.RemoteAddr // unparseable; only reachable with a synthetic request
	}
	if !tp.trusts(addr) {
		return addr.String()
	}

	// Walk the X-Forwarded-For chain from the right. Each proxy appends the
	// peer it saw, so the right-most entry is the one our trusted proxy
	// observed and the entries left of it are only as trustworthy as whoever
	// sent them. Skipping over trusted proxies and stopping at the first
	// address that isn't one yields the closest client we have any reason to
	// believe -- and never an address an attacker could have written, because
	// anything they prepend sits to the left of the entry the proxy appended
	// for their own connection.
	chain := forwardedFor(r)
	for i := len(chain) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil {
			break // malformed: everything further left is unverifiable
		}
		hop = hop.Unmap()
		if tp.trusts(hop) {
			continue
		}
		return hop.String()
	}

	// No usable forwarded address (no header, or a chain of nothing but
	// proxies). Fall back to the peer; throttling degrades to a shared bucket
	// rather than trusting something forgeable.
	return addr.String()
}

// forwardedFor flattens every X-Forwarded-For header line into one ordered
// list of hops. Splitting on commas is enough: the header is defined as a
// comma-separated list of addresses, with no quoting.
func forwardedFor(r *http.Request) []string {
	var chain []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		chain = append(chain, strings.Split(line, ",")...)
	}
	return chain
}

// IsHTTPS reports whether the browser reached binbash over TLS -- directly, or
// via a trusted proxy that terminated it. It decides whether the session cookie
// gets the Secure flag and whether HSTS is sent, so a false positive on a
// plain-HTTP LAN install would lock the user out (the browser refuses to store
// a Secure cookie sent over HTTP) and a false negative behind a proxy would
// leave the session cookie willing to travel in the clear.
func (tp *TrustedProxies) IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	addr, ok := peer(r)
	if !ok || !tp.trusts(addr) {
		return false
	}

	// The left-most value is the scheme the original client used; anything
	// after it describes a hop between proxies.
	proto := r.Header.Get("X-Forwarded-Proto")
	if comma := strings.IndexByte(proto, ','); comma >= 0 {
		proto = proto[:comma]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}
