package onboarding

import (
	"net/http"
	"strings"

	"github.com/NorthAIProject/north-client/internal/auth"
)

// RequireOnboarded redirects new accounts to first-run onboarding until they
// complete or skip it. Mount after RequireAuth on the /app route group.
func RequireOnboarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFrom(r.Context())
		if !ok || !user.NeedsOnboarding() {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/app/onboarding") {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/app/onboarding")
			w.WriteHeader(http.StatusOK)
			return
		}

		http.Redirect(w, r, "/app/onboarding", http.StatusSeeOther)
	})
}
