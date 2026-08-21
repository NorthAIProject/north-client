package mcpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
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

// stubAuth stands in for the connections service: a fixed map of token to
// account, and a count of how often it was asked.
type stubAuth struct {
	byToken map[string]users.User
	calls   int
}

func (a *stubAuth) Authenticate(_ context.Context, token string) (users.User, error) {
	a.calls++
	user, ok := a.byToken[token]
	if !ok {
		return users.User{}, apperr.ErrUnauthenticated
	}
	return user, nil
}

// stubUsers stands in for users.Service in StaticAuthenticator.
type stubUsers struct{ user users.User }

func (s stubUsers) ByID(_ context.Context, id uuid.UUID) (users.User, error) {
	if id != s.user.ID {
		return users.User{}, apperr.ErrNotFound
	}
	return s.user, nil
}

func newUser() users.User { return users.User{ID: uuid.New(), Email: "fernando@north.test"} }

// asUser builds a request that has already been through authenticate.
func asUser(user users.User) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	return req.WithContext(context.WithValue(req.Context(), userKey{}, user))
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
	h := guardOrigin(Config{AllowedOrigins: []string{"https://khepri.example"}}, discardLog(), okHandler(&reached))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Origin", "HTTPS://Khepri.Example")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !reached {
		t.Errorf("a configured origin was rejected: %d", rec.Code)
	}
}

func TestAuthenticateRejectsAWrongOrMissingToken(t *testing.T) {
	user := newUser()
	auth := &stubAuth{byToken: map[string]users.User{"correct-horse": user}}

	for name, header := range map[string]string{
		"absent":     "",
		"wrong":      "Bearer nope",
		"not bearer": "Basic c2VjcmV0",
		"empty":      "Bearer ",
	} {
		t.Run(name, func(t *testing.T) {
			var reached bool
			h := authenticate(Config{Auth: auth}, discardLog(), okHandler(&reached))

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
			// Phase-2 OAuth extends this header with a metadata pointer; a bare
			// 403 would leave a client with nowhere to go.
			if got := rec.Header().Get("WWW-Authenticate"); got == "" {
				t.Error("no WWW-Authenticate header on a 401")
			}
		})
	}
}

func TestAuthenticateAcceptsAnIssuedTokenAndActsAsItsOwner(t *testing.T) {
	user := newUser()
	auth := &stubAuth{byToken: map[string]users.User{"nk_good": user}}

	var got users.User
	h := authenticate(Config{Auth: auth}, discardLog(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = userFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer nk_good")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.ID != user.ID {
		t.Fatalf("acted as %s, want %s", got.ID, user.ID)
	}
}

// A rejection must not tell the caller how the server is configured.
func TestAuthenticateDoesNotLeakConfiguration(t *testing.T) {
	user := newUser()
	auth := &stubAuth{byToken: map[string]users.User{"correct-horse": user}}

	var reached bool
	h := authenticate(Config{Auth: auth}, discardLog(), okHandler(&reached))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	body := rec.Body.String()
	for _, leak := range []string{"MCP_USER_ID", "correct-horse", user.ID.String()} {
		if strings.Contains(body, leak) {
			t.Errorf("the response body mentions %q: %q", leak, body)
		}
	}
}

// StaticAuthenticator is what keeps cmd/mcp-server working unchanged, so its
// behaviour is pinned separately from the DB-backed path.
func TestStaticAuthenticatorAcceptsOnlyTheConfiguredToken(t *testing.T) {
	user := newUser()
	auth := StaticAuthenticator{Token: "correct-horse", UserID: user.ID, Users: stubUsers{user: user}}

	got, err := auth.Authenticate(context.Background(), "correct-horse")
	if err != nil {
		t.Fatalf("the configured token was rejected: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("acted as %s, want the configured %s", got.ID, user.ID)
	}

	for _, token := range []string{"", "nope", "correct-hors", "correct-horsee"} {
		if _, err := auth.Authenticate(context.Background(), token); !apperr.Is(err, apperr.ErrUnauthenticated) {
			t.Errorf("token %q: err = %v, want ErrUnauthenticated", token, err)
		}
	}
}

// ask_coach reaches a paid model on every call.
func TestThrottleStopsARunawayCaller(t *testing.T) {
	var reached int
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})

	h := throttle(Config{RequestsPerMinute: 3}, discardLog(), next)
	user := newUser()

	var throttled int
	for range 10 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, asUser(user))
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

// The regression the whole multi-user change exists to avoid: one person's
// runaway agent must not throttle everybody else.
func TestThrottleIsPerAccount(t *testing.T) {
	var reached int
	h := throttle(Config{RequestsPerMinute: 3}, discardLog(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	}))

	noisy, quiet := newUser(), newUser()

	for range 20 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, asUser(noisy))
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, asUser(quiet))

	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("one account exhausting its budget throttled another")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	_ = reached
}

// The unauthenticated caller must not be able to spend the authenticated
// caller's budget, which is why throttle sits inside authenticate.
func TestThrottleIsNotReachableBeforeAuthentication(t *testing.T) {
	user := newUser()
	auth := &stubAuth{byToken: map[string]users.User{"nk_good": user}}

	var reached int
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})

	stack := authenticate(Config{Auth: auth}, discardLog(),
		throttle(Config{RequestsPerMinute: 2}, discardLog(), next))

	// Burn far more than the budget with a bad token.
	for range 20 {
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	}
	if reached != 0 {
		t.Fatal("an unauthenticated request reached the handler")
	}

	// The real caller still has its full allowance.
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer nk_good")

	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	if rec.Code == http.StatusTooManyRequests {
		t.Error("unauthenticated requests consumed the authenticated budget")
	}
	if reached != 1 {
		t.Errorf("the authenticated request reached the handler %d times, want 1", reached)
	}
}

// throttleAnonymous exists so a token-guessing loop costs a map lookup rather
// than a database query on every attempt.
func TestAnonymousThrottleRunsBeforeAuthentication(t *testing.T) {
	auth := &stubAuth{byToken: map[string]users.User{}}

	var reached bool
	stack := throttleAnonymous(Config{}, discardLog(),
		authenticate(Config{Auth: auth}, discardLog(), okHandler(&reached)))

	var throttled int
	for range anonymousPerMinute * 2 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer nk_guess")
		stack.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			throttled++
		}
	}

	if throttled == 0 {
		t.Fatal("a flood of bad tokens was never throttled")
	}
	if auth.calls > anonymousPerMinute {
		t.Errorf("the store was asked %d times for %d allowed attempts; the flood reached the database",
			auth.calls, anonymousPerMinute)
	}
	if reached {
		t.Error("an unauthenticated request reached the handler")
	}
}

func TestHealthChecksNeedNoCredential(t *testing.T) {
	user := newUser()
	h := NewHandler(Config{
		Auth: &stubAuth{byToken: map[string]users.User{"nk_good": user}},
		Log:  discardLog(),
	})

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
		}
	}
}
