package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// The share target is a GET onto this page, so the text has to survive the
// query string and land in the box. Without this the manifest entry is a link
// to an empty form.
func TestCapturePagePrefillsFromAShare(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)

	shared := "slept 6h and drank 2L of water"
	req := httptest.NewRequest(http.MethodGet, "/app/capture?text="+url.QueryEscape(shared), nil)
	req.AddCookie(session)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /app/capture answered %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), shared) {
		t.Error("the shared text was not prefilled into the page")
	}
}

func TestCapturePageRequiresASession(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, "/app/capture", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("GET /app/capture was served without a session")
	}
}

// The commit half is a browser form, so unlike the JSON twin it must be behind
// CSRF. This is the mirror of TestCaptureAPIIsNotBehindCSRF: the two edges have
// opposite requirements and both are about where they are mounted.
func TestCaptureCommitIsBehindCSRF(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)

	for _, path := range []string{"/app/capture/parse", "/app/capture/commit"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("text=2L+water"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(session)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s with a session but no CSRF token answered %d, want 403", path, rec.Code)
		}
	}
}

// The whole point of the preview: a ticked row is written, and the page says so.
func TestCommittingAReviewedItemWritesIt(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)
	csrf := csrfToken(t, handler, session)

	form := url.Values{}
	form.Set("items[0].kind", "water")
	form.Set("items[0].source", "1337ml of water")
	form.Set("items[0].amount_ml", "1337")
	form.Set("items[0].include", "1")

	req := httptest.NewRequest(http.MethodPost, "/app/capture/commit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(middleware.CSRFHeaderName, csrf.Value)
	req.AddCookie(session)
	req.AddCookie(csrf)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("commit answered %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "1337 ml") {
		t.Errorf("the receipt does not mention the logged water: %s", rec.Body.String())
	}

	// And it really is on the care page, not merely echoed back.
	show := httptest.NewRequest(http.MethodGet, "/app/care", nil)
	show.AddCookie(session)
	shown := httptest.NewRecorder()
	handler.ServeHTTP(shown, show)

	if !strings.Contains(shown.Body.String(), "1337") {
		t.Error("the care page does not show the water the capture logged")
	}
}

// An unticked row is a row the person declined. Writing it anyway would make
// the checkbox a lie.
func TestAnUntickedItemIsNotWritten(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)
	csrf := csrfToken(t, handler, session)

	form := url.Values{}
	form.Set("items[0].kind", "water")
	form.Set("items[0].amount_ml", "1337")
	// no include

	req := httptest.NewRequest(http.MethodPost, "/app/capture/commit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(middleware.CSRFHeaderName, csrf.Value)
	req.AddCookie(session)
	req.AddCookie(csrf)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("commit answered %d, want 422 when nothing was ticked", rec.Code)
	}

	show := httptest.NewRequest(http.MethodGet, "/app/care", nil)
	show.AddCookie(session)
	shown := httptest.NewRecorder()
	handler.ServeHTTP(shown, show)

	if strings.Contains(shown.Body.String(), "1337") {
		t.Error("an unticked item was written anyway")
	}
}

// A value edited past its bounds in the hidden form is refused rather than
// clamped: the preview round-trips through the client, so the server revalidates.
func TestAnEditedValueIsRevalidated(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)
	csrf := csrfToken(t, handler, session)

	form := url.Values{}
	form.Set("items[0].kind", "water")
	form.Set("items[0].amount_ml", "999999")
	form.Set("items[0].include", "1")

	req := httptest.NewRequest(http.MethodPost, "/app/capture/commit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(middleware.CSRFHeaderName, csrf.Value)
	req.AddCookie(session)
	req.AddCookie(csrf)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("commit answered %d for an out-of-range amount, want 422", rec.Code)
	}
}
