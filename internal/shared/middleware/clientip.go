package middleware

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/labstack/echo/v5"
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

// IPExtractor returns the function that attributes a request to an address.
//
// It is Echo's X-Forwarded-For extractor with every implicit trust switched
// off: by default Echo believes any loopback, link-local or private peer, which
// on a cluster network is every pod, including one an attacker controls. Only
// the configured prefixes are believed here. Set it once on echo.Config and
// handlers read c.RealIP(); code still behind chi calls the function directly.
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
// So the walk starts at the peer and runs right to left — the rightmost entries
// were appended by infrastructure we control, and the first one that is not
// ours is the earliest hop we can still believe. Anything further left was
// written by the caller. An unparseable hop stops the walk at the peer.
func (t TrustedProxies) IPExtractor() echo.IPExtractor {
	options := []echo.TrustOption{
		echo.TrustLoopback(false),
		echo.TrustLinkLocal(false),
		echo.TrustPrivateNet(false),
	}
	for _, prefix := range t {
		prefix = prefix.Masked()
		options = append(options, echo.TrustIPRange(&net.IPNet{
			IP:   net.IP(prefix.Addr().AsSlice()),
			Mask: net.CIDRMask(prefix.Bits(), prefix.Addr().BitLen()),
		}))
	}
	return echo.ExtractIPFromXFFHeader(options...)
}
