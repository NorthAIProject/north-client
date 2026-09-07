package middleware

import (
	"context"
	"net/http"
)

// analyticsKey carries what the browser needs to report to PostHog.
//
// On the context rather than as a layout parameter because layout.Base takes a
// title and every page in the application calls it. Threading a key through
// would touch every one of them, and the alternative — a package-level
// variable in web/shared — is the global state the coding standards rule out.
// This is the arrangement i18n.WithLocale already uses, read the same way from
// inside a template.
var analyticsKey = ctxValue[AnalyticsConfig]{key: "analytics"}

// AnalyticsConfig is the client half of the PostHog configuration.
//
// The key is the same phc_… project key the server-side client uses: PostHog's
// project key is public by design, which is why turning on the browser half
// needs no new secret and no infrastructure change.
type AnalyticsConfig struct {
	APIKey string
	Host   string
}

// Enabled reports whether there is anything to report to. A deployment with no
// PostHog key renders no snippet at all, which is the behaviour every other
// optional integration here has.
func (c AnalyticsConfig) Enabled() bool {
	return c.APIKey != "" && c.Host != ""
}

// Analytics puts the browser's PostHog configuration on every request.
//
// Mount it before auth.LoadUser, alongside Locale: the snippet it feeds is
// rendered for signed-out visitors too, and the landing page is the one page
// whose numbers this exists to collect.
func Analytics(cfg AnalyticsConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithAnalytics(r.Context(), cfg)))
		})
	}
}

// WithAnalytics puts a configuration on a context directly, for the callers
// that have no request to decorate — a template test, or a page rendered
// outside the router.
func WithAnalytics(ctx context.Context, cfg AnalyticsConfig) context.Context {
	return analyticsKey.set(ctx, cfg)
}

// identityKey carries the signed-in account's id, so the browser can call
// posthog.identify with the same string the server reports as DistinctId.
//
// Set by auth.LoadUser, which is the one place that resolves a session. Set
// nowhere else: two sources for the same identity is how an account ends up
// split across two PostHog persons.
var identityKey = ctxValue[string]{key: "analytics_identity"}

// WithIdentity records who this request belongs to, for analytics.
func WithIdentity(ctx context.Context, id string) context.Context {
	return identityKey.set(ctx, id)
}

// IdentityFrom is the signed-in account's id, or empty for a visitor.
//
// Empty is a normal state, not a failure: it is every request to the landing
// page, and the browser stays anonymous until an identify call joins the two.
func IdentityFrom(ctx context.Context) string {
	id, _ := identityKey.get(ctx)
	return id
}

// AnalyticsFrom is the configuration for this request. The zero value, whose
// Enabled reports false, is the correct answer when nothing mounted the
// middleware — a template must render without analytics rather than fail.
func AnalyticsFrom(ctx context.Context) AnalyticsConfig {
	cfg, _ := analyticsKey.get(ctx)
	return cfg
}
