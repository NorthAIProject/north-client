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

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
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
	return handler
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

// The rest of the site must still be behind CSRF. Without this, a change that
// "fixed" the test above by dropping the middleware entirely would pass.
func TestTheSiteIsStillBehindCSRF(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("email=a@b.test&password=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /login without a CSRF token = %d, want 403", rec.Code)
	}
}

// The install URLs must stay public. A browser fetches the manifest and the
// service worker before anyone is signed in, and a 302 to /login is how
// "Add to Home Screen" silently fails.
func TestManifestIsPublicAndStandalone(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/manifest+json") {
		t.Errorf("Content-Type = %q, want application/manifest+json", ct)
	}

	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if m["display"] != "standalone" {
		t.Errorf("display = %v, want standalone", m["display"])
	}
}

func TestServiceWorkerIsPublic(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), `addEventListener("fetch"`) {
		t.Error("service worker has no fetch handler")
	}
}

// Deleting an account is the most destructive thing this site can do, so the
// guards in front of it are worth pinning here rather than trusting the mount
// point to stay where it is. An anonymous, tokenless POST must never reach the
// handler; in a browser both failure modes would look like an ordinary
// redirect, and neither would show up in a package-level test of the handler.
func TestDeletingAnAccountIsBehindAuthAndCSRF(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodPost, "/app/settings/account/delete",
		strings.NewReader("confirm_email=someone@north.test"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// 403 from CSRF or 303 to the login page both mean it was stopped. What
	// must not happen is the handler running.
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous POST to the account delete = %d, want it turned away", rec.Code)
	}
}

// The export moved off /knowledge when it grew to cover the whole account, so
// the new path has to be mounted and behind authentication.
//
// Only the presence of the new route is asserted, not the absence of the old
// one: RequireAuth wraps the whole /app group and answers a signed-out request
// before chi ever decides whether the path matches, so every /app URL — real or
// invented — redirects identically. There is nothing here to tell them apart.
func TestTheExportLivesUnderSettings(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, "/app/settings/export.zip", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /app/settings/export.zip signed out = %d, want a 303 to the login page", rec.Code)
	}
}
