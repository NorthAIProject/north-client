package main

import (
	"context"
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

	pool := testdb.New(t)

	registry := ai.NewRegistry()
	registry.Register(fake.Text("unused"))

	cfg := &config.Config{
		Env:             config.EnvDevelopment,
		BaseURL:         "http://north.test",
		SessionLifetime: time.Hour,
	}

	return routes(cfg, pool, registry, stubStorage{}, nil)
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
