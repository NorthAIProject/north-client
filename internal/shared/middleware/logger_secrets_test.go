package middleware_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// "No secrets in logs" is true today by habit rather than by construction —
// nothing in the request logger reaches for a header or a cookie. This pins
// that, so the change which starts logging headers for debugging has to argue
// with a test rather than slip through review.
func TestTheRequestLogNeverCarriesCredentials(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, nil))

	handler := middleware.RequestID(middleware.Logger(base)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// The handler logs the way every handler does. Whatever it adds
			// must not drag the request's credentials along with it.
			middleware.FromContext(r.Context()).Info("handled something")
			w.WriteHeader(http.StatusOK)
		})))

	req := httptest.NewRequest(http.MethodPost, "/app/settings/ai?api_key=QUERYSECRET", nil)
	req.Header.Set("Authorization", "Bearer HEADERSECRET")
	req.Header.Set("Cookie", "north_session=COOKIESECRET")
	req.Header.Set("X-Api-Key", "APIKEYSECRET")
	req.AddCookie(&http.Cookie{Name: "north_session", Value: "COOKIESECRET"})

	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	if !strings.Contains(out, "handled something") {
		t.Fatalf("the handler's log never appeared, so this test proves nothing: %s", out)
	}

	for _, secret := range []string{"HEADERSECRET", "COOKIESECRET", "APIKEYSECRET"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q reached the logs:\n%s", secret, out)
		}
	}
}

// The path is logged, and a query string is part of neither. Credentials do
// turn up in query strings — an OAuth code, a signed link, a key somebody
// pasted — so logging the raw URL would be the easy way to leak one.
func TestTheRequestLogRecordsThePathWithoutTheQueryString(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, nil))

	handler := middleware.Logger(base)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=OAUTHSECRET", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	if !strings.Contains(out, "/auth/google/callback") {
		t.Errorf("the path was not logged at all: %s", out)
	}
	if strings.Contains(out, "OAUTHSECRET") {
		t.Errorf("a query-string credential reached the logs:\n%s", out)
	}
}
