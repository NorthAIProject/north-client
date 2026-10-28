package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/apns"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/care"
	"github.com/NorthAIProject/north-client/internal/checkinreply"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/decisions"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/export"
	"github.com/NorthAIProject/north-client/internal/fitness"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/mcpauth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/news"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/planimport"
	"github.com/NorthAIProject/north-client/internal/progresschecks"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/settings"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/vault"
	"github.com/NorthAIProject/north-client/internal/voice"
	"github.com/NorthAIProject/north-client/internal/workouts"
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
func apiRouter(t *testing.T) *echo.Echo {
	t.Helper()
	sessions := resolverThatMustNotRun{t: t}
	e := echox.New(echox.Config{})
	mountAPI(e, sessions, apiSet{
		auth:         auth.NewAPI(sessions).WithAuthService(&auth.Service{}, &auth.Middleware{}),
		mcpAuth:      mcpauth.NewAPI(nil, nil, nil, nil, nil),
		capture:      capture.NewAPI(nil, nil, nil, nil),
		onboarding:   onboarding.NewAPI(nil),
		dashboard:    dashboard.NewAPI(nil),
		coach:        coach.NewAPI(nil, nil, nil),
		voice:        voice.NewAPI(nil, nil),
		exercises:    exercises.NewAPI(nil, nil),
		settings:     settings.NewAPI(nil),
		training:     workouts.NewAPI(nil),
		activity:     activity.NewAPI(nil),
		health:       health.NewAPI(nil),
		fitness:      fitness.NewAPI(nil, ""),
		insights:     insights.NewAPI(nil),
		goals:        goals.NewAPI(nil),
		checkins:     checkins.NewAPI(nil),
		checkinReply: checkinreply.NewAPI(nil),
		reports:      reports.NewAPI(nil),
		memories:     memories.NewAPI(nil),
		knowledge:    documents.NewAPI(nil, nil),
		formChecks:   media.NewAPI(nil, nil),
		care:         care.NewAPI(care.Options{}),
		mind:         mind.NewAPI(nil),
		nutrition:    meals.NewAPI(meals.HandlerOptions{}),
		planImport:   planimport.NewAPI(nil, nil),
		progress:     progresschecks.NewAPI(nil, nil, nil),
		decisions:    decisions.NewAPI(nil),
		nudges:       nudges.NewAPI(nil),
		devices:      apns.NewAPI(nil),
		export:       export.NewHandler(nil, nil, nil),
		// Enabled, so its routes are mounted and checked against the spec.
		vault:      vault.NewHandler(nil, true),
		calculator: calculator.NewAPI(nil, nil),
		news:       news.NewAPI(nil),
	})
	return e
}

// apiRoute is one registered method and path, with parameters written as
// {name} the way OpenAPI (and chi before Echo) writes them.
type apiRoute struct{ method, path string }

// apiRoutes lists every route Echo serves, leaving out the not-found
// fallbacks, which are not endpoints.
func apiRoutes(e *echo.Echo) []apiRoute {
	var out []apiRoute
	for _, ri := range e.Router().Routes() {
		if ri.Method == echo.RouteNotFound || ri.Method == echo.RouteAny {
			continue
		}
		segments := strings.Split(ri.Path, "/")
		for i, seg := range segments {
			if name, ok := strings.CutPrefix(seg, ":"); ok {
				segments[i] = "{" + name + "}"
			}
		}
		out = append(out, apiRoute{method: ri.Method, path: strings.Join(segments, "/")})
	}
	return out
}

// publicAPIPrefixes are the only /api/v1 paths reachable without a session:
// signing in, and capture, which checks its own nk_ connection token.
var publicAPIPrefixes = []string{"/api/v1/auth/", "/api/v1/capture/"}

// publicAPIRoutes are single public routes, named exactly so their siblings
// stay guarded: the OAuth returns, which prove themselves by their state.
var publicAPIRoutes = map[string]bool{
	"/api/v1/fitness/strava/callback": true,
	// Who sent an invite link, for the signed-out app; the inviter's name and
	// handle, nothing else.
	"/api/v1/invites/{code}": true,
	// The crew behind a /i/c/<code> link, for the signed-out web page; the
	// crew's id, name and member count, never its members or messages.
	"/api/v1/crews/join/{code}": true,
	// Facebook's OAuth return for Connect Facebook in the app; like Strava's,
	// it proves itself by its single-use state.
	"/api/v1/social/facebook/callback": true,
}

func isPublicAPIRoute(route string) bool {
	if publicAPIRoutes[route] {
		return true
	}
	for _, prefix := range publicAPIPrefixes {
		if strings.HasPrefix(route, prefix) {
			return true
		}
	}
	return false
}

// Every route added to /api/v1 lands behind auth.Bearer unless it is
// deliberately public. A new feature API mounted in the wrong group fails here.
func TestEveryPrivateAPIRouteRequiresABearerToken(t *testing.T) {
	router := apiRouter(t)

	checked := 0
	for _, rt := range apiRoutes(router) {
		method, route := rt.method, rt.path
		if isPublicAPIRoute(route) {
			continue
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
	}
	if checked == 0 {
		t.Fatal("walked no private routes; the bearer group is not mounted")
	}
}
