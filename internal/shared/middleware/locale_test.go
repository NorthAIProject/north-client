package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// capture records what the locale was by the time the handler ran.
func capture(t *testing.T, r *http.Request) (string, string) {
	t.Helper()

	var locale, path string
	h := middleware.Locale(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		locale = i18n.LocaleFrom(r.Context())
		path = middleware.Path(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), r)
	return locale, path
}

func TestLocaleComesFromTheHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "es-MX,es;q=0.9")

	if got, _ := capture(t, r); got != "es" {
		t.Errorf("locale = %q, want %q", got, "es")
	}
}

func TestLocaleFallsBackToEnglish(t *testing.T) {
	if got, _ := capture(t, httptest.NewRequest(http.MethodGet, "/", nil)); got != "en" {
		t.Errorf("locale = %q, want English", got)
	}
}

// The switcher has to post back to the page the visitor was reading, and templ
// hands components a context rather than a request.
func TestThePathIsCarriedForTheSwitcher(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/privacy?from=footer", nil)

	if _, got := capture(t, r); got != "/privacy?from=footer" {
		t.Errorf("path = %q, want the full request URI", got)
	}
}

// This is the ordering contract, asserted rather than trusted to a comment in
// cmd/web. Locale can only guess; auth.LoadUser overwrites the guess with the
// account's own setting. Mounted the other way round, a laptop's browser
// settings would override what somebody chose in Khepri.
func TestAnAccountSettingOverwritesTheGuess(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/app", nil)
	r.Header.Set("Accept-Language", "pt-BR")

	// Locale runs first and guesses from the header.
	var afterGuess, afterAccount string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		afterGuess = i18n.LocaleFrom(r.Context())

		// Standing in for auth.LoadUser, which does exactly this once it has
		// resolved a session.
		ctx := i18n.WithLocale(r.Context(), "es")
		afterAccount = i18n.LocaleFrom(ctx)
	})
	middleware.Locale(inner).ServeHTTP(httptest.NewRecorder(), r)

	if afterGuess != "pt-BR" {
		t.Errorf("the guess was %q, want the header's pt-BR", afterGuess)
	}
	if afterAccount != "es" {
		t.Errorf("after the account setting the locale was %q, want es", afterAccount)
	}
}

// A background job or a direct component call has no request behind it and
// must read English rather than panic.
func TestNoRequestReadsEnglish(t *testing.T) {
	if got := i18n.LocaleFrom(context.Background()); got != "en" {
		t.Errorf("locale = %q, want English", got)
	}
	if got := middleware.Path(context.Background()); got != "" {
		t.Errorf("path = %q, want empty", got)
	}
}
