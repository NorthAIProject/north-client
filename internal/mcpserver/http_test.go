package mcpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Internal rather than external test package: these exercise the middleware
// directly, so that a failure names the stage that let the request through
// rather than the whole handler stack.

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

// A real MCP client sends no Origin at all, so the absence of the header must
// not be treated as a browser.
func TestOriginGuardAllowsRequestsWithNoOrigin(t *testing.T) {
	var reached bool
	h := guardOrigin(Config{}, discardLog(), okHandler(&reached))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if !reached || rec.Code != http.StatusOK {
		t.Errorf("a request with no Origin was rejected: %d", rec.Code)
	}
}

// The attack this exists for: a page on the open web posting to a loopback
// server the victim is running.
func TestOriginGuardRejectsAnUnknownBrowserOrigin(t *testing.T) {
	var reached bool
	h := guardOrigin(Config{}, discardLog(), okHandler(&reached))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Origin", "https://evil.example")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if reached {
		t.Fatal("an unrecognised origin reached the handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestOriginGuardAllowsAConfiguredOrigin(t *testing.T) {
	var reached bool
	h := guardOrigin(Config{AllowedOrigins: []string{"https://north.example"}}, discardLog(), okHandler(&reached))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Origin", "HTTPS://North.Example")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !reached {
		t.Errorf("a configured origin was rejected: %d", rec.Code)
	}
}

func TestAuthenticateRejectsAWrongOrMissingToken(t *testing.T) {
	for name, header := range map[string]string{
		"absent":     "",
		"wrong":      "Bearer nope",
		"not bearer": "Basic c2VjcmV0",
		"empty":      "Bearer ",
	} {
		t.Run(name, func(t *testing.T) {
			var reached bool
			h := authenticate(Config{Token: "correct-horse"}, discardLog(), okHandler(&reached))

			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if reached {
				t.Fatal("the request reached the handler")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got == "" {
				t.Error("no WWW-Authenticate header on a 401")
			}
		})
	}
}

// A rejection must not tell the caller how the server is configured.
func TestAuthenticateDoesNotLeakConfiguration(t *testing.T) {
	var reached bool
	h := authenticate(Config{Token: "correct-horse"}, discardLog(), okHandler(&reached))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	body := rec.Body.String()
	for _, leak := range []string{"MCP_USER_ID", "correct-horse"} {
		if strings.Contains(body, leak) {
			t.Errorf("the response body mentions %q: %q", leak, body)
		}
	}
}

// ask_coach reaches a paid model on every call, behind one static bearer.
func TestThrottleStopsARunawayCaller(t *testing.T) {
	var reached int
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})

	h := throttle(Config{RequestsPerMinute: 3}, discardLog(), next)

	var throttled int
	for range 10 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		if rec.Code == http.StatusTooManyRequests {
			throttled++
			if rec.Header().Get("Retry-After") == "" {
				t.Error("a 429 carried no Retry-After")
			}
		}
	}

	if throttled == 0 {
		t.Fatal("ten calls against a limit of three were all allowed")
	}
	if reached == 0 {
		t.Fatal("the limiter allowed nothing through at all")
	}
}

// The unauthenticated caller must not be able to spend the authenticated
// caller's budget, which is why throttle sits inside authenticate.
func TestThrottleIsNotReachableBeforeAuthentication(t *testing.T) {
	var reached int
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})

	stack := authenticate(Config{Token: "correct-horse"}, discardLog(),
		throttle(Config{RequestsPerMinute: 2}, discardLog(), next))

	// Burn far more than the budget with a bad token.
	for range 20 {
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	}

	// The real caller still has its full allowance. It cannot get past
	// authenticate here without a Users service, but a 401 rather than a 429
	// proves the budget was not consumed.
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong-but-different")

	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	if rec.Code == http.StatusTooManyRequests {
		t.Error("unauthenticated requests consumed the rate budget")
	}
	if reached != 0 {
		t.Error("an unauthenticated request reached the handler")
	}
}

func TestHealthChecksNeedNoCredential(t *testing.T) {
	h := NewHandler(Config{Token: "correct-horse", Log: discardLog()})

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
		}
	}
}
