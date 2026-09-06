package auth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

func guarded(t *testing.T, cfg auth.ThrottleConfig) http.Handler {
	t.Helper()
	reached := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return auth.NewThrottle(cfg, nil).Guard(reached)
}

func attempt(h http.Handler, remote, email string) int {
	body := url.Values{"email": {email}, "password": {"whatever"}}.Encode()
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = remote
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func TestGuessingFromOneAddressIsRefused(t *testing.T) {
	t.Parallel()

	h := guarded(t, auth.ThrottleConfig{PerMinute: 3, PerEmailPerMinute: 100})

	for i := range 3 {
		// A different email each time, so only the address bucket is spent.
		if code := attempt(h, "203.0.113.9:5000", "person"+string(rune('a'+i))+"@north.test"); code != http.StatusOK {
			t.Fatalf("attempt %d was refused with %d while the budget still covered it", i+1, code)
		}
	}

	if code := attempt(h, "203.0.113.9:5000", "another@north.test"); code != http.StatusTooManyRequests {
		t.Fatalf("the 4th attempt returned %d, want 429", code)
	}
}

// The reason the email bucket exists: an address limit alone lets a botnet
// spread one account's guesses across many hosts and never meet a bound.
func TestGuessingOneAccountFromManyAddressesIsRefused(t *testing.T) {
	t.Parallel()

	h := guarded(t, auth.ThrottleConfig{PerMinute: 1000, PerEmailPerMinute: 3})

	for i := range 3 {
		if code := attempt(h, "203.0.113."+string(rune('1'+i))+":5000", "victim@north.test"); code != http.StatusOK {
			t.Fatalf("attempt %d refused with %d", i+1, code)
		}
	}

	if code := attempt(h, "198.51.100.77:5000", "victim@north.test"); code != http.StatusTooManyRequests {
		t.Fatalf("a 4th address guessing the same account returned %d, want 429", code)
	}
}

// Otherwise alternating the capitalisation buys a fresh allowance per spelling.
func TestTheEmailBucketIgnoresCase(t *testing.T) {
	t.Parallel()

	h := guarded(t, auth.ThrottleConfig{PerMinute: 1000, PerEmailPerMinute: 2})

	attempt(h, "203.0.113.1:5000", "victim@north.test")
	attempt(h, "203.0.113.2:5000", " VICTIM@North.Test ")

	if code := attempt(h, "203.0.113.3:5000", "Victim@north.test"); code != http.StatusTooManyRequests {
		t.Fatalf("a respelled address got a fresh budget: %d", code)
	}
}

func TestOneCallerBeingRefusedLeavesEveryoneElseAlone(t *testing.T) {
	t.Parallel()

	h := guarded(t, auth.ThrottleConfig{PerMinute: 2, PerEmailPerMinute: 100})

	for range 3 {
		attempt(h, "203.0.113.9:5000", "noisy@north.test")
	}
	if code := attempt(h, "203.0.113.9:5000", "noisy@north.test"); code != http.StatusTooManyRequests {
		t.Fatalf("setup failed: the noisy caller was not exhausted, got %d", code)
	}

	if code := attempt(h, "198.51.100.4:5000", "quiet@north.test"); code != http.StatusOK {
		t.Fatalf("an unrelated caller was refused with %d", code)
	}
}

// Without a trusted-proxy list every request behind an ingress shares one
// bucket, so the first few callers spend the budget for everybody. This is the
// failure the whole clientip change exists to prevent.
func TestBehindAProxyCallersAreBoundedSeparately(t *testing.T) {
	t.Parallel()

	trusted, err := middleware.ParseTrustedProxies("10.42.0.0/16")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	h := guarded(t, auth.ThrottleConfig{PerMinute: 2, PerEmailPerMinute: 100, TrustedProxies: trusted})

	send := func(caller, email string) int {
		body := url.Values{"email": {email}}.Encode()
		r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Forwarded-For", caller)
		r.RemoteAddr = "10.42.0.7:44001"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	for range 3 {
		send("198.51.100.1", "a@north.test")
	}
	if code := send("198.51.100.1", "a@north.test"); code != http.StatusTooManyRequests {
		t.Fatalf("the noisy caller was not bounded: %d", code)
	}

	if code := send("198.51.100.2", "b@north.test"); code != http.StatusOK {
		t.Fatalf("a second caller behind the same proxy was refused with %d — the limiter is keyed on the proxy", code)
	}
}

// A refusal has to tell a script when to come back, or it comes back immediately.
func TestARefusalCarriesRetryAfter(t *testing.T) {
	t.Parallel()

	h := guarded(t, auth.ThrottleConfig{PerMinute: 1, PerEmailPerMinute: 100})

	attempt(h, "203.0.113.9:5000", "a@north.test")

	body := url.Values{"email": {"a@north.test"}}.Encode()
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "203.0.113.9:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("code = %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After on a 429")
	}
}

// A request with no email — the passkey endpoints post JSON — must still pass
// the address bucket and must not be refused for having no account to key on.
func TestARequestWithoutAnEmailIsStillBoundedByAddress(t *testing.T) {
	t.Parallel()

	h := guarded(t, auth.ThrottleConfig{PerMinute: 2, PerEmailPerMinute: 1})

	send := func() int {
		r := httptest.NewRequest(http.MethodPost, "/auth/passkey/login/begin", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.RemoteAddr = "203.0.113.9:5000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	if code := send(); code != http.StatusOK {
		t.Fatalf("first passkey call refused with %d", code)
	}
	if code := send(); code != http.StatusOK {
		t.Fatalf("second passkey call refused with %d; the email bucket should not apply", code)
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("third passkey call returned %d, want 429 from the address bucket", code)
	}
}

// A nil throttle is the test wiring, and it must pass traffic rather than panic.
func TestANilThrottlePassesEverythingThrough(t *testing.T) {
	t.Parallel()

	var nilThrottle *auth.Throttle
	h := nilThrottle.Guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	if code := attempt(h, "203.0.113.9:5000", "a@north.test"); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
}
