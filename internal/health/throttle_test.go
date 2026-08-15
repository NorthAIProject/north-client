package health_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/health"
)

const oneReading = `{"readings":[{"metric":"steps","value":1,"unit":"count","started_at":"2026-08-15T02:00:00Z"}]}`

func send(h http.Handler, token, remoteAddr string) int {
	req := httptest.NewRequest(http.MethodPost, "/apple_health", strings.NewReader(oneReading))
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = remoteAddr

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// A token-guessing loop must be stopped before it reaches the database, which
// is the only reason a pre-authentication bound exists at all: the real
// per-account limit cannot run until the token has already been looked up.
func TestGuessingTokensIsThrottledBeforeAuthentication(t *testing.T) {
	svc, user := newService(t)
	h := health.NewHandler(health.HandlerConfig{
		Service:            svc,
		Auth:               stubAuth{token: "nk_testtoken", user: user},
		AnonymousPerMinute: 3,
	})

	for i := range 3 {
		if code := send(h, "nk_wrongguess", "203.0.113.5:1234"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, code)
		}
	}

	if code := send(h, "nk_wrongguess", "203.0.113.5:1234"); code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 — guessing was never throttled", code)
	}
}

// Keyed by address, so one noisy source cannot lock everybody else out.
func TestThrottlingOneAddressLeavesAnotherAddressWorking(t *testing.T) {
	svc, user := newService(t)
	h := health.NewHandler(health.HandlerConfig{
		Service:            svc,
		Auth:               stubAuth{token: "nk_testtoken", user: user},
		AnonymousPerMinute: 2,
	})

	for range 3 {
		send(h, "nk_wrongguess", "203.0.113.5:1234")
	}

	if code := send(h, "nk_wrongguess", "198.51.100.9:4321"); code == http.StatusTooManyRequests {
		t.Error("a second address was throttled because the first misbehaved")
	}
}

// The per-account bound is the real one: it survives a caller who changes
// address, because by this point the request carries a name.
func TestAnAuthenticatedCallerIsBoundedPerAccount(t *testing.T) {
	svc, user := newService(t)
	h := health.NewHandler(health.HandlerConfig{
		Service:            svc,
		Auth:               stubAuth{token: "nk_testtoken", user: user},
		RequestsPerMinute:  2,
		AnonymousPerMinute: 1000,
	})

	for i := range 2 {
		if code := send(h, "nk_testtoken", "203.0.113.5:1234"); code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i+1, code)
		}
	}

	// A different address, so only the account-keyed bound can catch this.
	if code := send(h, "nk_testtoken", "198.51.100.9:4321"); code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 — the per-account bound did not survive a change of address", code)
	}
}

func TestAThrottledResponseTellsTheClientWhenToRetry(t *testing.T) {
	svc, user := newService(t)
	h := health.NewHandler(health.HandlerConfig{
		Service:            svc,
		Auth:               stubAuth{token: "nk_testtoken", user: user},
		AnonymousPerMinute: 1,
	})

	send(h, "nk_wrongguess", "203.0.113.5:1234")

	req := httptest.NewRequest(http.MethodPost, "/apple_health", strings.NewReader(oneReading))
	req.Header.Set("Authorization", "Bearer nk_wrongguess")
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 with no Retry-After leaves a well-behaved client guessing")
	}
}
