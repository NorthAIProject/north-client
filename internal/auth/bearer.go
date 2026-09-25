package auth

import (
	"net/http"
	"strings"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// RequireBearer authenticates native-client requests by the session token in
// the Authorization header and puts the user in the request context, where
// UserFrom and MustUser find it exactly as they do behind the cookie
// middleware. It never reads the session cookie: a JSON route reachable by
// cookie would be reachable cross-site without the CSRF check the browser
// routes get.
//
// Failures are JSON, never a redirect to the login page.
func RequireBearer(sessions SessionResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				httpx.Error(w, apperr.ErrUnauthenticated, "A bearer token is required.")
				return
			}

			session, err := sessions.Resolve(r.Context(), token)
			if err != nil {
				if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
					httpx.Error(w, apperr.ErrUnauthenticated, "That token is not valid.")
					return
				}
				httpx.Error(w, apperr.ErrUnavailable, "Something went wrong.")
				return
			}

			// The account's language, as LoadUser does for the browser: the
			// coach, reports and error messages answer in it.
			ctx := ContextWithUser(r.Context(), session.User)
			ctx = i18n.WithLocale(ctx, string(session.User.Locale))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	return token, found && token != ""
}
