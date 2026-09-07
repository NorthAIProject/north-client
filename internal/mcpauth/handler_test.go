package mcpauth_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/mcpauth"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// machineRouter mounts the machine-facing endpoints the way cmd/web does.
func (h harness) machineRouter(t *testing.T) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	mcpauth.NewMachineHandler(h.svc, discardLog(), nil).Routes(r)
	return r
}

// The discovery endpoints are what a pasted URL turns into. Both spellings of
// the protected-resource path are served, because RFC 9728 specifies the
// suffixed one and several clients still ask for the bare path.
func TestDiscoveryIsServedAtBothPaths(t *testing.T) {
	h := newHarness(t)
	router := h.machineRouter(t)

	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d, want 200", path, rec.Code)
		}

		var doc mcpauth.ProtectedResourceMetadata
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s did not return the document: %v", path, err)
		}
		if doc.Resource != "https://north.test/mcp" {
			t.Errorf("%s reports resource %q", path, doc.Resource)
		}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("the authorization server document returned %d", rec.Code)
	}
}

// A browser-based client performs discovery and the token exchange from its
// own page, so these endpoints need CORS or they fail with nothing visible.
// Credentials must never be allowed: it is illegal with a wildcard origin, and
// none of these endpoints reads a cookie.
func TestTheMachineEndpointsAreCallableFromABrowser(t *testing.T) {
	h := newHarness(t)
	router := h.machineRouter(t)

	paths := []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-authorization-server",
		"/oauth/register",
		"/oauth/token",
		"/oauth/revoke",
	}

	for _, path := range paths {
		t.Run("preflight "+path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, path, nil))

			if rec.Code != http.StatusNoContent {
				t.Errorf("preflight returned %d, want 204", rec.Code)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("Access-Control-Allow-Origin is %q, want *", got)
			}
			if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
				t.Errorf("Access-Control-Allow-Credentials is set to %q; it must never be", got)
			}
			for _, header := range []string{"Content-Type", "Authorization", "MCP-Protocol-Version"} {
				if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), header) {
					t.Errorf("Access-Control-Allow-Headers omits %s", header)
				}
			}
		})
	}
}

func TestRegisterReturnsAPublicClient(t *testing.T) {
	h := newHarness(t)
	router := h.machineRouter(t)

	body := `{
		"client_name": "Claude Code",
		"redirect_uris": ["http://127.0.0.1:41000/cb"],
		"grant_types": ["authorization_code", "refresh_token"],
		"token_endpoint_auth_method": "none",
		"client_uri": "https://claude.ai",
		"logo_uri": "https://claude.ai/logo.png"
	}`

	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register returned %d: %s", rec.Code, rec.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["client_id"] == "" || out["client_id"] == nil {
		t.Error("no client_id in the response")
	}
	// Not an omission: every MCP client is public, and a secret it could not
	// keep would be a secret to leak.
	if _, present := out["client_secret"]; present {
		t.Error("the response carries a client_secret")
	}
	if out["token_endpoint_auth_method"] != "none" {
		t.Errorf("token_endpoint_auth_method is %v, want none", out["token_endpoint_auth_method"])
	}

	// client_uri and logo_uri are fields this server has no use for. Refusing
	// a registration for carrying them would fail every real client.
	if out["client_id_issued_at"] == nil {
		t.Error("no client_id_issued_at in the response")
	}
}

func TestRegisterRefusesAClientThatWantsASecret(t *testing.T) {
	h := newHarness(t)
	router := h.machineRouter(t)

	body := `{"client_name":"x","redirect_uris":["https://x.test/cb"],` +
		`"token_endpoint_auth_method":"client_secret_post"}`

	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register returned %d, want 400", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["error"] != "invalid_client_metadata" {
		t.Errorf("error is %v, want invalid_client_metadata", out["error"])
	}
}

// Open registration needs a bound, and the bound has to be visible rather than
// silent: an operator seeing 429s knows the endpoint is being farmed.
func TestRegistrationIsRateLimited(t *testing.T) {
	h := newHarness(t)
	router := h.machineRouter(t)

	var limited bool
	for i := 0; i < 40; i++ {
		body := `{"client_name":"c","redirect_uris":["https://x.test/cb"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
		req.RemoteAddr = "203.0.113.9:1234"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code == http.StatusTooManyRequests {
			limited = true
			if rec.Header().Get("Retry-After") == "" {
				t.Error("a 429 carried no Retry-After")
			}
			break
		}
	}
	if !limited {
		t.Error("forty registrations from one address were all accepted")
	}
}

// The token endpoint is form-encoded. Every client posts
// application/x-www-form-urlencoded, and a JSON-only decoder would fail all of
// them.
func TestTheTokenEndpointReadsAForm(t *testing.T) {
	h := newHarness(t)
	router := h.machineRouter(t)
	ctx := context.Background()

	client := h.client(t, "http://127.0.0.1:41000/cb")
	req, verifier := h.authorize(t, client, "")
	redirect, err := h.svc.Approve(ctx, req.ID, verifier, h.user.ID, "")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {codeFrom(t, redirect)},
		"client_id":     {client.ID},
		"redirect_uri":  {client.RedirectURIs[0]},
		"code_verifier": {testVerifier},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("token returned %d: %s", rec.Code, rec.Body.String())
	}

	// The body carries a bearer token, so a cache holding a copy is what
	// RFC 6749 section 5.1 forbids.
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control is %q, want no-store", got)
	}

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["token_type"] != "Bearer" {
		t.Errorf("token_type is %v, want Bearer", out["token_type"])
	}
	for _, key := range []string{"access_token", "refresh_token", "expires_in"} {
		if out[key] == nil || out[key] == "" {
			t.Errorf("no %s in the token response", key)
		}
	}
}

// Every token-endpoint refusal is one answer, for the reason the 401 on /mcp
// is one answer: a distinguishable message confirms which codes were real.
func TestTokenRefusalsAreOneAnswer(t *testing.T) {
	h := newHarness(t)
	router := h.machineRouter(t)

	post := func(form url.Values) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	client := h.client(t)

	cases := []url.Values{
		{
			"grant_type": {"authorization_code"}, "code": {"nope"}, "client_id": {client.ID},
			"redirect_uri": {client.RedirectURIs[0]}, "code_verifier": {testVerifier},
		},
		{"grant_type": {"refresh_token"}, "refresh_token": {"nope"}, "client_id": {client.ID}},
	}

	var first map[string]any
	for i, form := range cases {
		status, body := post(form)
		if status != http.StatusBadRequest {
			t.Errorf("case %d returned %d, want 400", i, status)
		}
		if body["error"] != "invalid_grant" {
			t.Errorf("case %d reports error %v, want invalid_grant", i, body["error"])
		}
		if first == nil {
			first = body
			continue
		}
		if body["error_description"] != first["error_description"] {
			t.Errorf("case %d describes its refusal as %v but the first said %v; they must match",
				i, body["error_description"], first["error_description"])
		}
	}

	t.Run("an unsupported grant is named as such", func(t *testing.T) {
		status, body := post(url.Values{"grant_type": {"password"}})
		if status != http.StatusBadRequest {
			t.Errorf("status is %d, want 400", status)
		}
		if body["error"] != "unsupported_grant_type" {
			t.Errorf("error is %v, want unsupported_grant_type", body["error"])
		}
	})
}

// RFC 7009: always 200, whether or not the token existed. Without this
// endpoint, removing a connector inside a client would leave a live grant only
// North's settings page could turn off.
func TestRevocationAlwaysSucceeds(t *testing.T) {
	h := newHarness(t)
	router := h.machineRouter(t)

	for name, form := range map[string]url.Values{
		"an unknown token": {"token": {"nk_not_a_real_token"}},
		"no token at all":  {},
		"an empty token":   {"token": {""}},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("revoke returned %d, want 200", rec.Code)
			}
		})
	}
}
