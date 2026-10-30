package export

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Routes mounts the export. Must be behind RequireAuth.
//
// Under /settings rather than /knowledge, where it started: it covers the whole
// account now, and a path that says knowledge would be describing a sixth of
// what comes out.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/settings/export.zip", func(w http.ResponseWriter, r *http.Request) { h.download(w, r, refusePlain) })
}

func refusePlain(w http.ResponseWriter) {
	http.Error(w, refusalMessage, http.StatusTooManyRequests)
}
