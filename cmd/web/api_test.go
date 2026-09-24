package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/onboarding"
)

// resolverThatMustNotRun fails the test if a request without a bearer token
// reaches session resolution: the middleware should turn it away first.
type resolverThatMustNotRun struct{ t *testing.T }

func (r resolverThatMustNotRun) Resolve(context.Context, string) (auth.Session, error) {
	r.t.Helper()
	r.t.Error("session resolved for a request that carried no bearer token")
	return auth.Session{}, nil
}

// apiRouter mounts /api/v1 exactly as the server does, with no database behind
// it. Only routing and authentication run; no handler body is reached.
func apiRouter(t *testing.T) chi.Router {
	t.Helper()
	sessions := resolverThatMustNotRun{t: t}
	r := chi.NewRouter()
	mountAPI(r, sessions, apiSet{
		auth:       auth.NewAPI(sessions).WithAuthService(&auth.Service{}, &auth.Middleware{}),
		capture:    capture.NewAPI(nil, nil, nil, nil),
		onboarding: onboarding.NewAPI(nil),
		dashboard:  dashboard.NewAPI(nil),
		coach:      coach.NewAPI(nil, nil, nil),
		exercises:  exercises.NewAPI(nil, nil),
	})
	return r
}

// publicAPIPrefixes are the only /api/v1 paths reachable without a session:
// signing in, and capture, which checks its own nk_ connection token.
var publicAPIPrefixes = []string{"/api/v1/auth/", "/api/v1/capture/"}

func isPublicAPIRoute(route string) bool {
	for _, prefix := range publicAPIPrefixes {
		if strings.HasPrefix(route, prefix) {
			return true
		}
	}
	return false
}

// Every route added to /api/v1 lands behind RequireBearer unless it is
// deliberately public. A new feature API mounted in the wrong group fails here.
func TestEveryPrivateAPIRouteRequiresABearerToken(t *testing.T) {
	router := apiRouter(t)

	checked := 0
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if isPublicAPIRoute(route) {
			return nil
		}
		checked++
		t.Run(method+" "+route, func(t *testing.T) {
			req := httptest.NewRequest(method, route, nil)
			// A browser session cookie is not a bearer token.
			req.AddCookie(&http.Cookie{Name: "north_session", Value: "from-the-browser"})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("content type = %q, want JSON", got)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("walked no private routes; the bearer group is not mounted")
	}
}
