package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Bearer authenticates native-client requests by the session token in the
// Authorization header and puts the user in the request context, where
// UserFrom and MustUser find it exactly as they do behind the cookie
// middleware. It never reads the session cookie: a JSON route reachable by
// cookie would be reachable cross-site without the CSRF check the browser
// routes get.
//
// Failures are JSON, never a redirect to the login page.
func Bearer(sessions SessionResolver) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			r := c.Request()
			token, ok := bearerToken(r)
			if !ok {
				return httpx.Fail(apperr.ErrUnauthenticated, "A bearer token is required.")
			}

			session, err := sessions.Resolve(r.Context(), token)
			if err != nil {
				if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
					return httpx.Fail(apperr.ErrUnauthenticated, "That token is not valid.")
				}
				return httpx.Fail(errors.Join(apperr.ErrUnavailable, err), "Something went wrong.")
			}

			// The account's language, as LoadUser does for the browser: the
			// coach, reports and error messages answer in it.
			ctx := ContextWithUser(r.Context(), session.User)
			ctx = i18n.WithLocale(ctx, string(session.User.Locale))
			c.SetRequest(r.WithContext(ctx))
			return next(c)
		}
	}
}

func bearerToken(r *http.Request) (string, bool) {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	return token, found && token != ""
}
