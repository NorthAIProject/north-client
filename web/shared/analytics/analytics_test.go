package analytics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/web/shared/analytics"
)

// resolve runs the real middleware chain for a path, because Resolve reads
// three separate context values and a test that set them by hand would pass
// even if the middleware stopped setting them.
func resolve(t *testing.T, target, identity string, cfg middleware.AnalyticsConfig) analytics.Config {
	t.Helper()

	var got analytics.Config
	h := middleware.Locale(middleware.Analytics(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if identity != "" {
				ctx = middleware.WithIdentity(ctx, identity)
			}
			got = analytics.Resolve(ctx)
		}),
	))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	return got
}

var configured = middleware.AnalyticsConfig{APIKey: "phc_test", Host: "https://ph.example"}

// A deployment with no PostHog key must render no snippet at all, the way
// every other optional integration here reports its own absence.
func TestNoKeyMeansNoSnippet(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]middleware.AnalyticsConfig{
		"nothing set": {},
		"no key":      {Host: "https://ph.example"},
		"no host":     {APIKey: "phc_test"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := resolve(t, "/", "", cfg); got.Enabled() {
				t.Fatalf("resolved an enabled config from %+v", cfg)
			}
		})
	}
}

// Autocapture records the text content of whatever was clicked. Inside /app
// that text is a goal title, a memory, or a check-in note, so the answer there
// is always no — and the password reset pages are signed out but carry a
// single-use token in the URL, which is why this is a fixed list rather than
// "anything outside /app".
func TestAutocaptureIsOnlyEverOnPublicPages(t *testing.T) {
	t.Parallel()

	on := []string{"/", "/signup", "/login", "/privacy", "/terms", "/oauth/authorize"}
	for _, path := range on {
		if got := resolve(t, path, "", configured); !got.Autocapture {
			t.Errorf("autocapture is off for %s, want on", path)
		}
	}

	off := []string{
		"/app", "/app/chat", "/app/goals", "/app/check-ins", "/app/knowledge",
		"/app/settings/connections", "/app/insights/body", "/app/mind/journal",
		// Signed out, and both carry a single-use token.
		"/forgot-password", "/reset-password",
	}
	for _, path := range off {
		if got := resolve(t, path, "", configured); got.Autocapture {
			t.Errorf("autocapture is on for %s, want off", path)
		}
	}
}

// The privacy policy promises no message content, no document text, and no
// health data. Replay of any page under /app breaks that by definition, so the
// recorded set is two signed-out pages and nothing else.
func TestReplayIsOnlyEverOnTheLandingAndConsentPages(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/", "/oauth/authorize"} {
		if got := resolve(t, path, "", configured); !got.Record {
			t.Errorf("replay is off for %s, want on", path)
		}
	}

	for _, path := range []string{
		"/app", "/app/chat", "/app/check-ins", "/app/knowledge", "/app/mind",
		"/signup", "/login", "/privacy", "/terms", "/reset-password",
	} {
		if got := resolve(t, path, "", configured); got.Record {
			t.Errorf("replay is on for %s, want off", path)
		}
	}
}

// The query string is attribution on the landing page and a liability
// everywhere else: /oauth/authorize carries a request_id and /reset-password a
// single-use token, and $current_url would carry either into a property.
func TestOnlyTheLandingPageKeepsItsQueryString(t *testing.T) {
	t.Parallel()

	if got := resolve(t, "/?utm_source=x&ref=y", "", configured); !got.KeepQuery {
		t.Error("the landing page dropped its query string, losing attribution")
	}

	for _, path := range []string{
		"/oauth/authorize?request_id=abc&state=def",
		"/reset-password?token=secret",
		"/signup?next=/app",
		"/app/chat?id=1",
	} {
		if got := resolve(t, path, "", configured); got.KeepQuery {
			t.Errorf("%s kept its query string, which can carry a token", path)
		}
	}
}

// A page reached with a trailing slash is the same page. Without this, "/"
// and "" disagree and a rule silently stops applying.
func TestTrailingSlashesAndQueriesResolveToTheSamePage(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"/", "/?a=1"} {
		got := resolve(t, target, "", configured)
		if !got.Autocapture || !got.Record {
			t.Errorf("%s did not resolve to the landing page: %+v", target, got)
		}
	}

	for _, target := range []string{"/signup", "/signup/", "/signup?next=/app"} {
		if got := resolve(t, target, "", configured); !got.Autocapture {
			t.Errorf("%s did not resolve to the signup page", target)
		}
	}
}

// The lists are exact matches, not prefixes. A future /signup-v2 or
// /terms-and-conditions must be opted in deliberately rather than inherit a
// neighbour's settings, and a route that merely begins with a public path must
// not pick up autocapture or replay by accident.
func TestAPathIsMatchedWholeRatherThanByPrefix(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/signup-v2", "/terms-and-conditions", "/loginhelp",
		"/privacy-policy", "/oauth/authorize/account", "/apples",
	} {
		got := resolve(t, path, "", configured)
		if got.Autocapture {
			t.Errorf("autocapture is on for %s by prefix, want off", path)
		}
		if got.Record {
			t.Errorf("replay is on for %s by prefix, want off", path)
		}
	}
}

// The identify call is what joins an anonymous landing session to the account
// it became. The string has to be the account id, because that is what the
// server sends as PostHog's DistinctId.
func TestIdentityIsCarriedThroughWhenSignedIn(t *testing.T) {
	t.Parallel()

	const id = "6f1b6f38-0d1f-4f8e-9c2f-2b7c9a0e1d55"

	if got := resolve(t, "/app", id, configured); got.Identity != id {
		t.Errorf("identity is %q, want %q", got.Identity, id)
	}

	// A visitor stays anonymous, which is the normal state on every public
	// page and must not resolve to some placeholder.
	if got := resolve(t, "/", "", configured); got.Identity != "" {
		t.Errorf("a signed-out visitor resolved identity %q, want empty", got.Identity)
	}
}
