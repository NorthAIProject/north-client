package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies are the networks whose X-Forwarded-For header may be believed.
//
// Empty means believe nobody, which is the correct answer on a laptop and the
// safe answer anywhere it has not been configured: an unconfigured deployment
// throttles by the address it can see rather than by one a caller can choose.
type TrustedProxies []netip.Prefix

// ParseTrustedProxies reads a comma-separated list of CIDRs.
//
// A bare address is accepted and read as a single host, because "10.42.0.7" is
// what an operator reaches for and demanding "/32" only produces a typo.
func ParseTrustedProxies(raw string) (TrustedProxies, error) {
	var out TrustedProxies

	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		if prefix, err := netip.ParsePrefix(field); err == nil {
			out = append(out, prefix)
			continue
		}

		addr, err := netip.ParseAddr(field)
		if err != nil {
			return nil, fmt.Errorf("middleware: %q is not an address or CIDR", field)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}

	return out, nil
}

func (t TrustedProxies) contains(addr netip.Addr) bool {
	// Unmap first: a proxy that connects over IPv6 to an IPv4 listener arrives
	// as ::ffff:10.42.0.7, which matches no IPv4 prefix until it is unwrapped.
	addr = addr.Unmap()
	for _, prefix := range t {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP returns the address a request should be attributed to.
//
// # Why this exists
//
// Behind an ingress controller, r.RemoteAddr is the proxy, not the caller.
// Every rate limiter keyed on it therefore shares one bucket across the whole
// internet: the limit stops being a bound on any one caller and becomes a
// switch that one caller can flip for everybody. That failure is invisible in
// development, where there is no proxy and RemoteAddr is right.
//
// # Why the header is not simply trusted
//
// X-Forwarded-For is caller-supplied. Reading it unconditionally is worse than
// ignoring it: an attacker sets a fresh value per request and gets an unlimited
// number of buckets, which is a rate limiter that rate-limits nobody.
//
// So the header is consulted only when the peer is itself trusted, and the walk
// runs right to left — the rightmost entries were appended by infrastructure we
// control, and the first one that is not ours is the earliest hop we can still
// believe. Anything further left was written by the caller.
func ClientIP(r *http.Request, trusted TrustedProxies) string {
	peer := remoteAddr(r)
	if len(trusted) == 0 {
		return peer
	}

	addr, err := netip.ParseAddr(peer)
	if err != nil || !trusted.contains(addr) {
		// The connection did not come from a proxy we know. Whatever it claims
		// about earlier hops is its own invention.
		return peer
	}

	forwarded := r.Header.Values("X-Forwarded-For")
	for i := len(forwarded) - 1; i >= 0; i-- {
		hops := strings.Split(forwarded[i], ",")
		for j := len(hops) - 1; j >= 0; j-- {
			hop, hopErr := netip.ParseAddr(strings.TrimSpace(hops[j]))
			if hopErr != nil {
				// Unparseable means forged or truncated. Stop rather than skip:
				// continuing would step past it to a value the same writer
				// chose.
				return peer
			}
			if !trusted.contains(hop) {
				return hop.Unmap().String()
			}
		}
	}

	// Every hop was one of ours, which happens for in-cluster traffic. The peer
	// is then the most specific true thing available.
	return peer
}

func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
