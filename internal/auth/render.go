package auth

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// render writes a templ component with an explicit status code.
//
// templ.Handler always writes 200, but a rejected form has to answer 422 and a
// failed login 401 for the response to be honest about what happened.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := c.Render(r.Context(), w); err != nil {
		// The status line is already sent, so this cannot become a 500. Log it
		// and let the client see a truncated page.
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

// tokenExpiry is when the session cookie should expire, matching the lifetime
// the session store used when it created the row.
func tokenExpiry(svc *Service) time.Time {
	return time.Now().Add(svc.Sessions().Lifetime())
}
