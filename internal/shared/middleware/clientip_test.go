package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

func TestWithoutTrustedProxiesTheHeaderIsIgnored(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "203.0.113.9:51234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := middleware.ClientIP(r, nil); got != "203.0.113.9" {
		t.Fatalf("ClientIP = %q, want the peer address", got)
	}
}

// The whole point of the trust check. A caller that could set its own key would
// get one bucket per request, which is a rate limiter that bounds nothing.
func TestAnUntrustedPeerCannotForgeItsAddress(t *testing.T) {
	t.Parallel()

	trusted, err := middleware.ParseTrustedProxies("10.42.0.0/16")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "203.0.113.9:51234"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")

	if got := middleware.ClientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("ClientIP = %q, want the peer address for an untrusted peer", got)
	}
}

func TestATrustedProxyRevealsTheCaller(t *testing.T) {
	t.Parallel()

	trusted, _ := middleware.ParseTrustedProxies("10.42.0.0/16")

	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "10.42.0.7:44001"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")

	if got := middleware.ClientIP(r, trusted); got != "198.51.100.1" {
		t.Fatalf("ClientIP = %q, want the forwarded caller", got)
	}
}

// A caller may prepend anything it likes before the real hops. The walk runs
// right to left and stops at the first address the infrastructure did not write.
func TestTheCallerCannotPrependAFakeHop(t *testing.T) {
	t.Parallel()

	trusted, _ := middleware.ParseTrustedProxies("10.42.0.0/16")

	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "10.42.0.7:44001"
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 198.51.100.1, 10.42.0.3")

	if got := middleware.ClientIP(r, trusted); got != "198.51.100.1" {
		t.Fatalf("ClientIP = %q, want the last untrusted hop", got)
	}
}

func TestRepeatedHeadersAreWalkedInOrder(t *testing.T) {
	t.Parallel()

	trusted, _ := middleware.ParseTrustedProxies("10.42.0.0/16")

	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "10.42.0.7:44001"
	r.Header.Add("X-Forwarded-For", "198.51.100.1")
	r.Header.Add("X-Forwarded-For", "10.42.0.3")

	if got := middleware.ClientIP(r, trusted); got != "198.51.100.1" {
		t.Fatalf("ClientIP = %q", got)
	}
}

// Stopping rather than skipping: continuing past a forged entry would step to a
// value written by the same hand.
func TestAnUnparseableHopFallsBackToThePeer(t *testing.T) {
	t.Parallel()

	trusted, _ := middleware.ParseTrustedProxies("10.42.0.0/16")

	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "10.42.0.7:44001"
	r.Header.Set("X-Forwarded-For", "not-an-address, 10.42.0.3")

	if got := middleware.ClientIP(r, trusted); got != "10.42.0.7" {
		t.Fatalf("ClientIP = %q, want the peer", got)
	}
}

// A proxy speaking IPv6 to an IPv4 listener arrives mapped. Without unmapping,
// no IPv4 prefix matches and the trust check silently never fires.
func TestAnIPv4MappedProxyIsStillTrusted(t *testing.T) {
	t.Parallel()

	trusted, _ := middleware.ParseTrustedProxies("10.42.0.0/16")

	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "[::ffff:10.42.0.7]:44001"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")

	if got := middleware.ClientIP(r, trusted); got != "198.51.100.1" {
		t.Fatalf("ClientIP = %q, want the forwarded caller", got)
	}
}

func TestWhenEveryHopIsOursThePeerIsUsed(t *testing.T) {
	t.Parallel()

	trusted, _ := middleware.ParseTrustedProxies("10.42.0.0/16")

	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "10.42.0.7:44001"
	r.Header.Set("X-Forwarded-For", "10.42.0.3")

	if got := middleware.ClientIP(r, trusted); got != "10.42.0.7" {
		t.Fatalf("ClientIP = %q", got)
	}
}

func TestParseAcceptsBareAddressesAndRejectsNonsense(t *testing.T) {
	t.Parallel()

	trusted, err := middleware.ParseTrustedProxies(" 10.42.0.7 , 192.168.0.0/16 ,, ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(trusted) != 2 {
		t.Fatalf("parsed %d prefixes, want 2", len(trusted))
	}

	if _, err := middleware.ParseTrustedProxies("traefik.local"); err == nil {
		t.Error("a hostname was accepted; a bad config must be loud at boot")
	}
}
