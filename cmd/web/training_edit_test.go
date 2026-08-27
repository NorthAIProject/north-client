package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Editing a training plan added five routes, three of which change data. What
// they do to a plan is covered in internal/workouts against a real database;
// what cannot be tested there is how they are mounted — whether they sit behind
// authentication and CSRF, and what status a malformed path gets.
//
// That is a composition invariant, the same reason the MCP and Telegram
// mounting tests live in this file: the only thing that can break it is
// somebody moving a route in routes().

// signIn creates an account and returns the session cookie for it.
//
// Through the auth service rather than the signup form: these tests are about
// where the training routes are mounted, and driving a multi-step HTML signup
// to reach them would fail for reasons that have nothing to do with that.
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

	return &http.Cookie{Name: auth.SessionCookieName, Value: token}
}

// editRoutes are every route that changes a plan. They are listed once so a
// route added later without a mounting test is a visible omission rather than
// an invisible one.
func editRoutes(planID uuid.UUID) []string {
	id := planID.String()
	return []string{
		"/app/training/" + id + "/days/0/exercises",
		"/app/training/" + id + "/days/0/exercises/0/swap",
		"/app/training/" + id + "/days/0/exercises/0/remove",
		"/app/training/" + id + "/days/0/exercises/0/move",
		"/app/training/" + id + "/days/0/exercises/0/sets",
	}
}

// An unauthenticated edit must never reach the handler. The plan id in the
// path belongs to nobody, so a route that answered it at all would be
// answering for whoever asked.
func TestTrainingEditRoutesRequireASession(t *testing.T) {
	handler := testRoutes(t)

	for _, path := range editRoutes(uuid.New()) {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Redirected to the login page, or refused outright — either is a
		// request that did not reach the plan. A 2xx would mean it did.
		if rec.Code >= 200 && rec.Code < 300 {
			t.Errorf("POST %s answered %d without a session", path, rec.Code)
		}
	}
}

// The edit routes change data from a browser holding a cookie, which is exactly
// what CSRF defends. Mounting one outside that middleware would let any site
// rewrite someone's training plan.
func TestTrainingEditRoutesAreBehindCSRF(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)

	for _, path := range editRoutes(uuid.New()) {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(session)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s with a session but no CSRF token answered %d, want 403", path, rec.Code)
		}
	}
}

// With both credentials the request reaches the handler, and is then answered
// on its merits — here, a plan id that belongs to nobody.
//
// This is what proves the CSRF result above is about the token rather than
// about the route being unreachable in tests.
func TestAnAuthenticatedEditWithACSRFTokenReachesTheHandler(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)
	csrf := csrfToken(t, handler, session)

	for _, path := range editRoutes(uuid.New()) {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("catalog_slug=squat"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set(middleware.CSRFHeaderName, csrf.Value)
		req.AddCookie(session)
		req.AddCookie(csrf)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Errorf("POST %s was still 403 with a matching token", path)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s answered %d for a plan that does not exist, want 404", path, rec.Code)
		}
	}
}

// The day and exercise indices are read out of the path. A value that cannot be
// a position has to be refused before it reaches a slice.
func TestAMalformedEditPathIsRefused(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)
	csrf := csrfToken(t, handler, session)
	id := uuid.New().String()

	cases := map[string]int{
		"/app/training/" + id + "/days/-1/exercises":            http.StatusUnprocessableEntity,
		"/app/training/" + id + "/days/nope/exercises":          http.StatusUnprocessableEntity,
		"/app/training/" + id + "/days/0/exercises/-1/remove":   http.StatusUnprocessableEntity,
		"/app/training/" + id + "/days/0/exercises/nope/remove": http.StatusUnprocessableEntity,
		"/app/training/not-a-uuid/days/0/exercises/0/remove":    http.StatusNotFound,
	}

	for path, want := range cases {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set(middleware.CSRFHeaderName, csrf.Value)
		req.AddCookie(session)
		req.AddCookie(csrf)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != want {
			t.Errorf("POST %s answered %d, want %d", path, rec.Code, want)
		}
	}
}

// csrfToken performs a safe request to collect the token cookie the middleware
// issues, which is how a browser gets one before submitting anything.
func csrfToken(t *testing.T, handler http.Handler, session *http.Cookie) *http.Cookie {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			return c
		}
	}

	t.Fatalf("no %s cookie was issued by a safe request", middleware.CSRFCookieName)
	return nil
}

// /app/training/plans is a static segment sharing a level with
// /app/training/{id}. If chi ever resolved the param route first, "plans" would
// be parsed as a plan id, fail, and answer 404 — so the status here is what
// proves the mounting.
func TestThePlansListIsNotSwallowedByThePlanRoute(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	session := signIn(t, pool)

	req := httptest.NewRequest(http.MethodGet, "/app/training/plans", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("GET /app/training/plans answered 404: \"plans\" was parsed as a plan id")
	}

	// The account has no plans, so the handler sends them to the intake — which
	// is only reachable if the request got as far as listPlans.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/app/training/new" {
		t.Errorf("redirected to %q, want /app/training/new", got)
	}
}

func TestThePlansListRequiresASession(t *testing.T) {
	handler := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, "/app/training/plans", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Location"); strings.Contains(got, "/app/training") {
		t.Errorf("an unauthenticated request was sent to %q rather than to log in", got)
	}
}
