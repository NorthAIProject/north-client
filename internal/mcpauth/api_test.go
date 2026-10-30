package mcpauth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/mcpauth"
	"github.com/NorthAIProject/north-client/internal/mcpserver"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/users"
)

// apiRouter mounts the consent API as cmd/api does: the public half bare, the
// decision behind Bearer.
func (h harness) apiRouter(t *testing.T) (http.Handler, *auth.SessionStore) {
	t.Helper()
	userSvc := users.NewService(users.NewRepository(h.pool))
	sessions := auth.NewSessionStore(h.pool, time.Hour)
	authSvc := auth.NewService(userSvc, sessions, auth.ServiceOptions{BaseURL: baseURL})
	onboardingSvc := onboarding.NewService(userSvc,
		memories.NewService(memories.NewRepository(h.pool)),
		goals.NewService(goals.NewRepository(h.pool)))
	api := mcpauth.NewAPI(h.svc, authSvc, auth.NewMiddleware(nil), onboardingSvc, discardLog())

	e := echox.New(echox.Config{})
	api.RegisterPublic(e.Group(""))
	api.Register(e.Group("", auth.Bearer(sessions)))
	return e, sessions
}

func call(t *testing.T, h http.Handler, method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return v
}

func beginBody(client mcpauth.Client, redirectURI string) string {
	b, _ := json.Marshal(mcpauth.AuthorizeRequest{
		ClientID:            client.ID,
		RedirectURI:         redirectURI,
		ResponseType:        "code",
		State:               "opaque-state",
		CodeChallenge:       testChallenge(),
		CodeChallengeMethod: mcpauth.ChallengeMethodS256,
	})
	return string(b)
}

// The screen's whole journey over JSON: park, read back with the verifier,
// create the account on the screen, approve, and the client's callback gets a
// code it can exchange.
func TestConsentAPICreatesAnAccountAndApproves(t *testing.T) {
	h := newHarness(t)
	client := h.client(t)
	router, _ := h.apiRouter(t)

	rec := call(t, router, http.MethodPost, "/auth/oauth/authorize", beginBody(client, client.RedirectURIs[0]), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("begin status = %d: %s", rec.Code, rec.Body)
	}
	begun := decode[mcpauth.AuthorizeResponse](t, rec)
	if begun.Request == nil || begun.Verifier == "" {
		t.Fatalf("begin = %+v", begun)
	}
	if begun.Request.ClientName != "Claude Code" || begun.Request.RedirectHost != "claude.ai" {
		t.Fatalf("request = %+v", begun.Request)
	}
	base := "/auth/oauth/requests/" + begun.Request.ID

	// Bound to the browser: no verifier, or the wrong one, is the same 404.
	for _, verifier := range []string{"", "not-the-cookie"} {
		rec = call(t, router, http.MethodGet, base, "", map[string]string{mcpauth.VerifierHeader: verifier})
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "oauth_request_expired") {
			t.Fatalf("verifier %q: status = %d body = %s", verifier, rec.Code, rec.Body)
		}
	}
	rec = call(t, router, http.MethodGet, base, "", map[string]string{mcpauth.VerifierHeader: begun.Verifier})
	if rec.Code != http.StatusOK {
		t.Fatalf("show status = %d: %s", rec.Code, rec.Body)
	}

	// A field error comes back per field, as the Go card showed it.
	bad, _ := json.Marshal(mcpauth.ConsentAccountRequest{Verifier: begun.Verifier, Email: "ana@north.test", Password: "short", DisplayName: "Ana"})
	rec = call(t, router, http.MethodPost, base+"/account", string(bad), nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"password"`) {
		t.Fatalf("weak password: status = %d body = %s", rec.Code, rec.Body)
	}

	good, _ := json.Marshal(mcpauth.ConsentAccountRequest{
		Verifier: begun.Verifier, Email: "ana@north.test", Password: "correct horse battery staple",
		DisplayName: "Ana", Timezone: "Europe/Lisbon",
	})
	rec = call(t, router, http.MethodPost, base+"/account", string(good), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("account status = %d: %s", rec.Code, rec.Body)
	}
	session := decode[auth.AuthResponse](t, rec)
	if session.Token == "" {
		t.Fatal("no session token")
	}
	// Seeded for the agent: an account made here skips the wizard.
	if session.User.NeedsOnboarding {
		t.Error("an account created on the consent screen should be onboarded")
	}

	rec = call(t, router, http.MethodGet, base, "", map[string]string{mcpauth.VerifierHeader: begun.Verifier})
	if !decode[mcpauth.ConsentRequest](t, rec).AccountCreated {
		t.Error("the request does not record that it created the account")
	}

	decision := `{"verifier":"` + begun.Verifier + `","readOnly":true}`
	// The decision needs the person.
	if rec = call(t, router, http.MethodPost, "/oauth/requests/"+begun.Request.ID+"/approve", decision, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("approve without bearer: status = %d", rec.Code)
	}
	rec = call(t, router, http.MethodPost, "/oauth/requests/"+begun.Request.ID+"/approve", decision,
		map[string]string{"Authorization": "Bearer " + session.Token})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d: %s", rec.Code, rec.Body)
	}
	redirect := decode[mcpauth.ConsentDecisionResponse](t, rec).Redirect
	if !strings.HasPrefix(redirect, client.RedirectURIs[0]) {
		t.Fatalf("redirect = %q", redirect)
	}
	code := codeFrom(t, redirect)
	if u, _ := url.Parse(redirect); u.Query().Get("state") != "opaque-state" {
		t.Errorf("state not returned: %s", redirect)
	}

	tokens, err := h.svc.ExchangeCode(t.Context(), mcpauth.CodeExchange{
		Code: code, ClientID: client.ID, RedirectURI: client.RedirectURIs[0], CodeVerifier: testVerifier,
	})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if tokens.Scope != mcpserver.ScopeRead {
		t.Errorf("read-only approval granted %q", tokens.Scope)
	}

	// Consumed: a second approve is the expired answer.
	rec = call(t, router, http.MethodPost, "/oauth/requests/"+begun.Request.ID+"/approve", decision,
		map[string]string{"Authorization": "Bearer " + session.Token})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("replayed approve: status = %d", rec.Code)
	}
}

func TestConsentAPIDenyReportsTheRefusalToTheClient(t *testing.T) {
	h := newHarness(t)
	client := h.client(t)
	router, sessions := h.apiRouter(t)

	begun := decode[mcpauth.AuthorizeResponse](t, call(t, router, http.MethodPost, "/auth/oauth/authorize", beginBody(client, client.RedirectURIs[0]), nil))
	token, _, err := sessions.Create(t.Context(), h.user.ID, auth.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	rec := call(t, router, http.MethodPost, "/oauth/requests/"+begun.Request.ID+"/deny", `{"verifier":"`+begun.Verifier+`"}`,
		map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusOK {
		t.Fatalf("deny status = %d: %s", rec.Code, rec.Body)
	}
	if redirect := decode[mcpauth.ConsentDecisionResponse](t, rec).Redirect; !strings.Contains(redirect, "error=access_denied") {
		t.Fatalf("redirect = %q", redirect)
	}
}

// Begin splits failures the way the Go page does: an untrusted destination
// is shown, never redirected to; a bad request to a known callback goes back
// to it.
func TestConsentAPIBeginSplitsFatalFromRedirectable(t *testing.T) {
	h := newHarness(t)
	client := h.client(t)
	router, _ := h.apiRouter(t)

	rec := call(t, router, http.MethodPost, "/auth/oauth/authorize", beginBody(client, "https://evil.example/cb"), nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "oauth_fatal") {
		t.Fatalf("unregistered callback: status = %d body = %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "evil.example") {
		t.Error("the untrusted destination was echoed back")
	}

	body := strings.Replace(beginBody(client, client.RedirectURIs[0]), `"responseType":"code"`, `"responseType":"token"`, 1)
	rec = call(t, router, http.MethodPost, "/auth/oauth/authorize", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad response type: status = %d body = %s", rec.Code, rec.Body)
	}
	got := decode[mcpauth.AuthorizeResponse](t, rec)
	if got.Request != nil || !strings.HasPrefix(got.Redirect, client.RedirectURIs[0]) || !strings.Contains(got.Redirect, "unsupported_response_type") {
		t.Fatalf("response = %+v", got)
	}
}
