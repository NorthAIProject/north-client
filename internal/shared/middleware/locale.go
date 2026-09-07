package middleware

import (
	"context"
	"net/http"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// pathKey carries the request's own path so a component can point back at it.
//
// It rides with Locale rather than in a middleware of its own because the one
// thing that needs it is the language switcher: a form that has to return the
// visitor to the page they were reading, and templ hands components a context
// rather than a request.
var pathKey = ctxValue[string]{key: "request_path"}

// Locale puts a language on every request, including the ones with nobody
// signed in.
//
// It must be mounted BEFORE auth.LoadUser, not after. This middleware can only
// guess — from an explicit cookie, then from Accept-Language — and LoadUser
// overwrites that guess with the account's own setting the moment it resolves a
// session. Mounted the other way round, a signed-in user reading Khepri in
// Spanish would be served Portuguese by their laptop's browser settings.
func Locale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := i18n.WithLocale(r.Context(), i18n.FromRequest(r))
		ctx = pathKey.set(ctx, r.URL.RequestURI())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// WithPath overrides the path a component links back to.
//
// For a page rendered in response to a request whose own URL should not be
// reflected back into it — the OAuth error page, where the query string
// contains an unvalidated redirect_uri that the language switcher would
// otherwise carry in a form action.
func WithPath(ctx context.Context, path string) context.Context {
	return pathKey.set(ctx, path)
}

// Path is the request's path and query, for a component that has to link or
// post back to it. Empty when nothing set it, which a caller should treat as
// "no known page" rather than as the root.
func Path(ctx context.Context) string {
	p, _ := pathKey.get(ctx)
	return p
}
