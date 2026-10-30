package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
)

func publicRouter(api *auth.API) http.Handler {
	e := echox.New(echox.Config{})
	api.RegisterPublic(e.Group(""))
	return e
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The sign-in pages show only what works: the options mirror the service's
// own switches.
func TestAuthOptionsReportTheConfiguredMethods(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		opts auth.ServiceOptions
		want auth.AuthOptionsResponse
	}{
		{"nothing optional", auth.ServiceOptions{}, auth.AuthOptionsResponse{Google: false, PasswordReset: true}},
		{"google", auth.ServiceOptions{GoogleClientID: "web", GoogleClientSecret: "secret"}, auth.AuthOptionsResponse{Google: true, PasswordReset: true}},
		{"production without a mailer", auth.ServiceOptions{Production: true}, auth.AuthOptionsResponse{Google: false, PasswordReset: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := auth.NewService(nil, nil, tc.opts)
			r := publicRouter(auth.NewAPI(fakeSessionResolver{}).WithAuthService(svc, &auth.Middleware{}))

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/options", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var got auth.AuthOptionsResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			// Passkeys depend on the relying party, not on these options.
			got.Passkey = false
			if got != tc.want {
				t.Fatalf("options = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGoogleWebRoutesWithoutGoogleAreJSON404(t *testing.T) {
	t.Parallel()

	r := publicRouter(auth.NewAPI(fakeSessionResolver{}).WithAuthService(&auth.Service{}, &auth.Middleware{}))
	for _, path := range []string{"/auth/google/start", "/auth/google/code"} {
		rec := postJSON(t, r, path, `{"code":"x"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Fatalf("%s content type = %q, want JSON", path, got)
		}
	}
}

// The front end serves the callback, so Google must be told to send the code
// there; the state comes back for the front end to keep.
func TestGoogleStartPointsGoogleAtTheFrontEndsCallback(t *testing.T) {
	t.Parallel()

	svc := auth.NewService(nil, nil, auth.ServiceOptions{GoogleClientID: "web", GoogleClientSecret: "secret", BaseURL: "https://kheprios.com"})
	r := publicRouter(auth.NewAPI(fakeSessionResolver{}).WithAuthService(svc, &auth.Middleware{}))

	rec := postJSON(t, r, "/auth/google/start", `{"redirectUri":"https://web.kheprios.com/auth/google/callback"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got auth.GoogleStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.State == "" {
		t.Fatal("no state returned")
	}
	u, err := url.Parse(got.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("redirect_uri") != "https://web.kheprios.com/auth/google/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") != got.State {
		t.Errorf("state in url = %q, returned %q", q.Get("state"), got.State)
	}
	if q.Get("client_id") != "web" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if strings.Contains(got.URL, "secret") {
		t.Error("the client secret leaked into the consent URL")
	}

	// No body: the server's own callback, as the Go page uses.
	rec = postJSON(t, r, "/auth/google/start", ``)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty body status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), url.QueryEscape("https://kheprios.com/auth/google/callback")) {
		t.Errorf("default redirect missing from %s", rec.Body)
	}
}

func TestGoogleWebRoutesRefuseAForeignRedirect(t *testing.T) {
	t.Parallel()

	svc := auth.NewService(nil, nil, auth.ServiceOptions{GoogleClientID: "web", GoogleClientSecret: "secret"})
	r := publicRouter(auth.NewAPI(fakeSessionResolver{}).WithAuthService(svc, &auth.Middleware{}))

	for _, redirect := range []string{
		"https://evil.example/steal",
		"http://evil.example/auth/google/callback",
		"https://kheprios.com/auth/google/callback?next=x",
		"/auth/google/callback",
	} {
		rec := postJSON(t, r, "/auth/google/start", `{"redirectUri":"`+redirect+`"}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("start %q status = %d, want 422", redirect, rec.Code)
		}
		rec = postJSON(t, r, "/auth/google/code", `{"code":"c","redirectUri":"`+redirect+`"}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("code %q status = %d, want 422", redirect, rec.Code)
		}
	}

	rec := postJSON(t, r, "/auth/google/start", `{"redirectUri":"http://localhost:5173/auth/google/callback"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("localhost callback status = %d, want 200", rec.Code)
	}

	rec = postJSON(t, r, "/auth/google/code", `{"code":"  "}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("blank code status = %d, want 422", rec.Code)
	}
}

func TestResetPasswordIsOffWhenNoMailCanBeSent(t *testing.T) {
	t.Parallel()

	svc := auth.NewService(nil, nil, auth.ServiceOptions{Production: true})
	r := publicRouter(auth.NewAPI(fakeSessionResolver{}).WithAuthService(svc, &auth.Middleware{}))
	rec := postJSON(t, r, "/auth/reset-password", `{"token":"t","password":"p","passwordConfirmation":"p"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

var resetTokenPattern = regexp.MustCompile(`/reset-password\?token=([A-Za-z0-9_%\-]+)`)

// The emailed link still lands on /reset-password?token=…, and the token in
// it is what the API takes: one use, then refused.
func TestResetPasswordFromTheEmailedLink(t *testing.T) {
	svc, sessions, _, mailer := newService(t)
	ctx := t.Context()

	if _, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{}); err != nil {
		t.Fatalf("signup: %v", err)
	}
	if err := svc.RequestPasswordReset(ctx, "fernando@north.test"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	if len(mailer.Messages) != 1 {
		t.Fatalf("sent %d messages, want 1", len(mailer.Messages))
	}
	match := resetTokenPattern.FindStringSubmatch(mailer.Messages[0].Body)
	if match == nil {
		t.Fatalf("no reset link in %q", mailer.Messages[0].Body)
	}
	token, err := url.QueryUnescape(match[1])
	if err != nil {
		t.Fatal(err)
	}

	r := publicRouter(auth.NewAPI(sessions).WithAuthService(svc, auth.NewMiddleware(nil)))

	body := `{"token":"` + token + `","password":"` + goodPassword + `x","passwordConfirmation":"mismatch"}`
	if rec := postJSON(t, r, "/auth/reset-password", body); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mismatch status = %d, want 422: %s", rec.Code, rec.Body)
	}

	body = `{"token":"` + token + `","password":"` + goodPassword + `x","passwordConfirmation":"` + goodPassword + `x"}`
	rec := postJSON(t, r, "/auth/reset-password", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got auth.AuthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Token == "" || got.User.Email != "fernando@north.test" {
		t.Fatalf("response = %+v", got)
	}

	rec = postJSON(t, r, "/auth/reset-password", body)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "invalid_reset_token") {
		t.Fatalf("reused link: status = %d body = %s", rec.Code, rec.Body)
	}
}
