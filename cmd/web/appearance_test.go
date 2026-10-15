package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/theme"
	"github.com/NorthAIProject/north-client/internal/users"
)

type setterSpy struct {
	calls []string
}

func (s *setterSpy) SetAppearance(_ context.Context, _ uuid.UUID, value, surface string) (bool, error) {
	s.calls = append(s.calls, value+"/"+surface)
	return true, nil
}

func postAppearance(t *testing.T, svc appearanceSetter, form url.Values, script bool, user *users.User) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/appearance", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if script {
		req.Header.Set("X-CSRF-Token", "token")
	}
	if user != nil {
		req = req.WithContext(auth.ContextWithUser(req.Context(), *user))
	}
	rec := httptest.NewRecorder()
	setAppearance(svc)(rec, req)
	return rec
}

func appearanceCookie(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == theme.CookieName {
			return c.Value
		}
	}
	return ""
}

// A visitor's choice lives in the cookie only; nothing is written to an
// account that does not exist.
func TestAVisitorsSwitcherSetsTheCookieAndAnswersTheScriptWith204(t *testing.T) {
	spy := &setterSpy{}
	rec := postAppearance(t, spy, url.Values{"appearance": {"light"}}, true, nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
	if got := appearanceCookie(rec); got != "light" {
		t.Fatalf("cookie = %q, want light", got)
	}
	if len(spy.calls) != 0 {
		t.Fatalf("saved %v for a visitor", spy.calls)
	}
}

// Without a script the form posts normally and is sent back where it was.
func TestAPlainFormPostIsRedirectedBack(t *testing.T) {
	rec := postAppearance(t, &setterSpy{}, url.Values{"appearance": {"dark"}, "next": {"/privacy"}}, false, nil)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/privacy" {
		t.Fatalf("status %d location %q, want 303 to /privacy", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAnOffsiteNextIsNotFollowed(t *testing.T) {
	rec := postAppearance(t, &setterSpy{}, url.Values{"appearance": {"dark"}, "next": {"https://evil.example"}}, false, nil)

	if rec.Header().Get("Location") != "/" {
		t.Fatalf("location %q, want /", rec.Header().Get("Location"))
	}
}

// Signed in, the account is written too: LoadUser prefers it, so a cookie
// alone would be overruled on the very next page.
func TestASignedInSwitchSavesToTheAccount(t *testing.T) {
	spy := &setterSpy{}
	user := users.User{ID: uuid.New()}
	rec := postAppearance(t, spy, url.Values{"appearance": {"system"}}, true, &user)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
	if len(spy.calls) != 1 || spy.calls[0] != "system/web_switcher" {
		t.Fatalf("saved %v, want system from web_switcher", spy.calls)
	}
	if got := appearanceCookie(rec); got != "system" {
		t.Fatalf("cookie = %q, want system", got)
	}
}

func TestAnUnknownAppearanceIsRefused(t *testing.T) {
	spy := &setterSpy{}
	user := users.User{ID: uuid.New()}
	rec := postAppearance(t, spy, url.Values{"appearance": {"sepia"}}, true, &user)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "system, light or dark") {
		t.Fatalf("body %q does not say what is allowed", rec.Body.String())
	}
	if len(spy.calls) != 0 || appearanceCookie(rec) != "" {
		t.Fatal("a refused value was saved")
	}
}
