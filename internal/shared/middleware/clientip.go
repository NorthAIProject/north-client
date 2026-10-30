package middleware

import (
	"fmt"
	"net"
	"net/http"
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
	fromHeader := echo.ExtractIPFromXFFHeader(options...)
	return func(r *http.Request) string {
		return behindCloudflare(r, fromHeader(r))
	}
}

// cloudflareEdge are the addresses Cloudflare's edge connects from, as published
// at https://www.cloudflare.com/ips-v4 and /ips-v6 (read 2026-10-30). The list
// changes rarely; an edge missing from it only means that edge's requests are
// keyed on the edge, as every request was before this list existed.
var cloudflareEdge = func() []netip.Prefix {
	ranges := []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	}
	out := make([]netip.Prefix, len(ranges))
	for i, r := range ranges {
		out[i] = netip.MustParsePrefix(r)
	}
	return out
}()

// behindCloudflare names the caller when the hop found above is Cloudflare's
// edge rather than the caller.
//
// # Why
//
// kheprios.com is proxied by Cloudflare, so the earliest hop the walk can
// believe is a Cloudflare edge, shared by everyone that edge serves. Every
// address-keyed limiter would then count strangers together.
//
// # Why CF-Connecting-IP may be believed here and nowhere else
//
// Cloudflare sets CF-Connecting-IP on every request it proxies, overwriting
// whatever the client sent, so a request that really arrived from the edge
// carries Cloudflare's word for the caller. A request from anywhere else could
// carry any value, which is why the header is read only when the hop is in
// cloudflareEdge. For subrequests from this zone's own Worker (the web app's
// server), Cloudflare takes the value from the X-Real-IP the Worker sets, which
// is the browser's address; a Worker on another zone gets one shared address
// (2a06:98c0:3600::103) and cannot choose it.
func behindCloudflare(r *http.Request, hop string) string {
	addr, err := netip.ParseAddr(hop)
	if err != nil {
		return hop
	}
	addr = addr.Unmap()
	edge := false
	for _, prefix := range cloudflareEdge {
		if prefix.Contains(addr) {
			edge = true
			break
		}
	}
	if !edge {
		return hop
	}
	caller, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("CF-Connecting-IP")))
	if err != nil {
		return hop
	}
	return caller.Unmap().String()
}
