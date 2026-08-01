package coach

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// render writes a templ component with an explicit status code, so a rejected
// message can answer 422 rather than a misleading 200.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}
