package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/web/shared/layout"
)

// knownNonDestinations are /app GET routes that are deliberately absent from
// the destination registry.
//
// The deny-list exists so the reverse check below can be exhaustive. Without
// it, "every page is registered" could only be asserted against a distinction
// — page versus fragment — that the routing code does not carry, and the test
// would have to be a warning nobody reads.
//
// Adding a route here is a decision. Adding a page is not: that goes in the
// registry, which is what makes it reachable.
var knownNonDestinations = map[string]string{
	// HTMX partials. Fragments of a page, not pages.
	"/app/panels":                 "dashboard range swap",
	"/app/nudges/bell":            "topbar poll",
	"/app/knowledge/passages":     "document passage fragment",
	"/app/insights/timeline/body": "insights range swap",
	"/app/insights/body/body":     "insights range swap",
	"/app/insights/mind/body":     "insights range swap",
	"/app/insights/progress/body": "insights range swap",
	"/app/insights/training/body": "insights range swap",

	// Actions with side effects. "Go to page" must not start an export.
	"/app/settings/export.zip": "quota-consuming download",

	// OAuth endpoints. Reached by pressing Connect on the fitness hub, and
	// meaningless to open directly: connect mints a state cookie and
	// redirects to Strava, callback expects a code and a matching state.
	"/app/fitness/strava/connect":  "oauth redirect",
	"/app/fitness/strava/callback": "oauth return",

	// Mounted only outside production, so listing it would ship a guaranteed
	// 404. See the note in layout.Destinations.
	"/app/settings/vault": "development-only feature",

	// Redirects to /app/insights/timeline rather than rendering.
	"/app/insights": "redirect",

	// Onboarding is a gate, reached by being new rather than by navigating.
	"/app/onboarding":      "onboarding gate",
	"/app/onboarding/done": "onboarding gate",
}

// appGETRoutes is every literal (parameterless) GET path under /app.
func appGETRoutes(t *testing.T, handler http.Handler) map[string]bool {
	t.Helper()

	mux, ok := handler.(*chi.Mux)
	if !ok {
		t.Fatalf("routes() returned %T, not a *chi.Mux", handler)
	}

	found := map[string]bool{}
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet {
			return nil
		}
		if !strings.HasPrefix(route, "/app") {
			return nil
		}
		// A route with a parameter cannot be a destination: there is nothing
		// to put in place of the id.
		if strings.ContainsAny(route, "{}*") {
			return nil
		}
		// chi joins Route("/app") with Get("/") into "/app/". The dashboard is
		// linked as "/app" everywhere else.
		route = strings.TrimSuffix(route, "/")
		if route == "" {
			route = "/app"
		}
		found[route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return found
}

// Building the router wires the whole application, which is slow. All three
// checks share one build rather than paying for it three times.
func TestTheDestinationRegistryMatchesTheRouter(t *testing.T) {
	routes := appGETRoutes(t, testRoutes(t))

	registered := map[string]bool{}
	for _, d := range layout.Destinations() {
		registered[d.Href] = true
	}

	// The rot this feature is most likely to suffer: someone renames a route
	// and the palette keeps offering the old one, which 404s.
	t.Run("every destination resolves to a real route", func(t *testing.T) {
		for _, d := range layout.Destinations() {
			if !routes[d.Href] {
				t.Errorf("%q points at %s, which is not a registered GET route", d.Label, d.Href)
			}
		}
	})

	// And the other direction, which is what actually keeps the palette
	// complete: a new page fails this test until it is either registered or
	// deliberately denied. Without it the registry would drift the way the
	// sidebar already did.
	t.Run("every page is a destination or denied on purpose", func(t *testing.T) {
		for route := range routes {
			if registered[route] {
				continue
			}
			if _, denied := knownNonDestinations[route]; denied {
				continue
			}
			t.Errorf("%s is reachable but is not in layout.Destinations(); "+
				"add it there, or add it to knownNonDestinations with a reason", route)
		}
	})

	// A deny-list that outlives the route it names is how a page becomes
	// invisible twice: once by not being registered, and once by an entry
	// that says it was considered.
	t.Run("the deny-list has no stale entries", func(t *testing.T) {
		for route, why := range knownNonDestinations {
			if !routes[route] {
				t.Errorf("knownNonDestinations lists %s (%s), which is no longer a route", route, why)
			}
			if registered[route] {
				t.Errorf("%s is both a destination and denied (%s)", route, why)
			}
		}
	})
}
