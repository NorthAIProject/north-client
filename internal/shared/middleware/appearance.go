package middleware

import (
	"net/http"

	"github.com/NorthAIProject/north-client/internal/shared/theme"
)

// Appearance puts an appearance on every request, including the ones with
// nobody signed in: the visitor's cookie, or the default.
//
// Mounted before auth.LoadUser for the reason Locale is. This can only read the
// cookie; LoadUser replaces the answer with the account's own setting when it
// has one, and the other order would let a cookie override an account.
func Appearance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := theme.WithAppearance(r.Context(), theme.Effective("", theme.FromCookie(r)))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
