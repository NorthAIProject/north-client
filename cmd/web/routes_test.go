package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// stubStorage satisfies media.Storage without a bucket. Nothing in these tests
// touches object storage; routes() only needs something non-nil to wire.
type stubStorage struct{}

func (stubStorage) Put(context.Context, string, string, io.Reader) error { return nil }
func (stubStorage) Get(context.Context, string) (io.ReadCloser, error)   { return nil, nil }
func (stubStorage) Delete(context.Context, string) error                 { return nil }
func (stubStorage) SignedURL(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func testRoutes(t *testing.T) http.Handler {
	t.Helper()
	return testRoutesWith(t, func(*config.Config) {})
}

func testRoutesWith(t *testing.T, configure func(*config.Config)) http.Handler {
	t.Helper()

	handler, _ := testRoutesAndPool(t, configure)
	return handler
}

// testRoutesAndPool also hands back the database, for tests that need to put a
// signed-in user behind the cookie.
func testRoutesAndPool(t *testing.T, configure func(*config.Config)) (http.Handler, *pgxpool.Pool) {
	t.Helper()

	pool := testdb.New(t)

	registry := ai.NewRegistry()
	registry.Register(fake.Text("unused"))

	cfg := &config.Config{
		Env:             config.EnvDevelopment,
		BaseURL:         "http://north.test",
		SessionLifetime: time.Hour,
	}
	configure(cfg)

	// nil metrics: these tests exercise routing, and a nil registry is a
	// supported configuration that counts nothing.
	handler, _ := routes(cfg, pool, registry, stubStorage{}, nil, nil, nil)
	return handler, pool
}

// The MCP endpoint must not sit behind the CSRF middleware.
//
// CSRF is a browser defence: it requires back a token the server put in a form.
// An MCP client has no form and no cookie — it presents a bearer token, which a
// browser never attaches on its own, so there is nothing to defend against and
// nothing for the client to send. Behind that middleware every call would be
// answered 403 with an HTML body no MCP client can read, and the symptom would
// look like a broken credential rather than a routing mistake.
//
// This is a composition invariant rather than a behaviour of any one package,
// which is why it is tested here: the only thing that can break it is somebody
// moving the mount in routes().
func TestMCPEndpointIsNotBehindCSRF(t *testing.T) {
	handler := testRoutes(t)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatal("POST /mcp was answered 403: the endpoint is behind CSRF again")
	}
	// 401 is the right answer to a request with no bearer token, and proves the
	// request reached the MCP handler's own authentication rather than being
	// turned away before it.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 from the MCP authenticator", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("no WWW-Authenticate challenge; phase-2 OAuth extends this header")
	}
}

// The Telegram webhook is the third endpoint that must sit outside CSRF, for
// the same reason as the first two: Telegram is not a browser, holds no cookie,
// and proves itself with a header instead. Behind that middleware every
// delivery would be answered 403, Telegram would retry forever, and the symptom
// would look like a broken bot rather than a routing mistake.
func TestTelegramWebhookIsNotBehindCSRF(t *testing.T) {
	handler := testRoutesWith(t, func(cfg *config.Config) {
		cfg.Telegram = config.TelegramConfig{
			BotToken:      "test-token",
			WebhookSecret: "test-secret",
		}
	})

	body := `{"update_id":1,"message":{"chat":{"id":1},"text":"hi"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatal("POST /webhooks/telegram was answered 403: the endpoint is behind CSRF again")
	}
	// 401 proves the request reached the webhook's own secret check rather than
	// being turned away before it: no secret header was sent.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 from the webhook's secret check", rec.Code)
	}
}

// Without a bot token there is nothing to deliver to, so the route must not
// exist at all — an endpoint that accepts updates nobody can answer is worse
// than no endpoint.
func TestTelegramWebhookIsAbsentWithoutABotToken(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK || rec.Code == http.StatusUnauthorized {
		t.Fatalf("status = %d: the webhook is mounted on a deployment with no bot", rec.Code)
	}
}

// The session middleware is equally wrong for /mcp: it would resolve a cookie
// that has nothing to do with the presented token. A browser that happens to be
// signed in must not be able to reach the endpoint on that basis alone.
func TestMCPEndpointIgnoresASessionCookie(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "north_session", Value: "whatever-a-browser-happens-to-hold"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: a session cookie is not an MCP credential", rec.Code)
	}
}

// The capture API is the fourth endpoint that must sit outside CSRF and the
// session middleware, for the same reason as /mcp and /ingest/health: the
// caller holds a bearer token, not a cookie, and there is no form to carry a
// CSRF token back. The composition is what breaks — moving the mount inside the
// session group turns every call into a 403 with an HTML body no client can
// read — so the invariant is pinned here rather than in the slice.
func TestCaptureAPIIsNotBehindCSRF(t *testing.T) {
	handler := testRoutes(t)

	for _, path := range []string{"/api/v1/capture/parse", "/api/v1/capture/commit"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"text":"2L water"}`))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusForbidden {
				t.Fatalf("POST %s was answered 403: the endpoint is behind CSRF again", path)
			}
			// 401 proves the request reached the API's own authentication
			// rather than being turned away before it.
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 from the capture authenticator", rec.Code)
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("no WWW-Authenticate challenge")
			}
		})
	}
}

// A session cookie is not a capture token. If the API ever accepted one it
// would be reachable from a browser, and the reason it can skip CSRF would stop
// being true.
func TestCaptureAPIDoesNotAcceptASessionCookie(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/capture/parse", strings.NewReader(`{"text":"2L water"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(session)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: a session cookie must not authenticate the capture API", rec.Code)
	}
}

// signIn creates an account and returns the session cookie for it.
//
// Through the auth service rather than the signup endpoint: these tests are
// about where routes are mounted, and driving a signup to reach them would fail
// for reasons that have nothing to do with that.
func signIn(t *testing.T, pool *pgxpool.Pool) *http.Cookie {
	t.Helper()

	userSvc := users.NewService(users.NewRepository(pool))
	sessions := auth.NewSessionStore(pool, time.Hour)
	svc := auth.NewService(userSvc, sessions, auth.ServiceOptions{
		BaseURL: "http://north.test",
	})

	user, token, err := svc.Signup(t.Context(), auth.SignupInput{
		Email:                "ada@north.test",
		DisplayName:          "Ada",
		Password:             "correct-horse-battery-staple",
		PasswordConfirmation: "correct-horse-battery-staple",
		Timezone:             "UTC",
	}, auth.Metadata{})
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}

	// A fresh account still needs onboarding, and RequireOnboarded sends it to
	// /app/onboarding before any app route is reached — which is a redirect,
	// not a refusal, and would make every assertion below pass for the wrong
	// reason.
	if _, err := userSvc.MarkOnboarded(t.Context(), user.ID); err != nil {
		t.Fatalf("mark onboarded: %v", err)
	}

	return &http.Cookie{Name: "north_session", Value: token}
}

// The kubelet's probe. It reaches the database, so a pod that cannot is taken
// out of rotation rather than left failing every request.
func TestHealthzReportsTheDatabase(t *testing.T) {
	handler := testRoutes(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got, want := rec.Body.String(), `{"status":"ok","database":"ok"}`; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// The operator count exists only when a secret guards it, and answers only to
// that secret.
func TestInternalMetricsIsGuardedBySecret(t *testing.T) {
	get := func(handler http.Handler, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/internal/metrics", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := get(testRoutes(t), "anything"); rec.Code != http.StatusNotFound {
		t.Fatalf("without METRICS_SECRET: status = %d, want 404", rec.Code)
	}

	handler := testRoutesWith(t, func(cfg *config.Config) { cfg.MetricsSecret = "s3cret" })
	if rec := get(handler, "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: status = %d, want 401", rec.Code)
	}
	rec := get(handler, "s3cret")
	if rec.Code != http.StatusOK {
		t.Fatalf("right secret: status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), `{"users":`) {
		t.Errorf("body = %q, want the user count", rec.Body.String())
	}
}

// Nothing outside the machine routes is served here any more: the pages
// belong to the web app. A path nobody routes is the API's JSON 404, not a
// page.
func TestAnUnknownPathIsAJSONNotFound(t *testing.T) {
	handler := testRoutes(t)

	for _, path := range []string{"/", "/app", "/login", "/api/v1/no-such-thing"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error.Message == "" {
			t.Errorf("GET %s: body is not the JSON error shape (%v)", path, err)
		}
	}
}
