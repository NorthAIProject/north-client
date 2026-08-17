package pwa

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Files is the install surface: the web app manifest, the service worker, and
// the page the worker shows when a navigation cannot reach the server.
//
// They live here rather than under /assets because a worker's default scope
// is its directory. /assets/js/sw.js cannot control /app.
//
//go:embed manifest.webmanifest sw.js offline.html
var Files embed.FS

// Mount registers the public install URLs. They must stay outside
// RequireAuth: a browser evaluates the manifest and the worker before anyone
// is signed in.
func Mount(r chi.Router) {
	r.Get("/manifest.webmanifest", serve("manifest.webmanifest", "application/manifest+json"))
	r.Get("/sw.js", serve("sw.js", "application/javascript; charset=utf-8"))

	// Public for the same reason the worker is: it is precached before anyone
	// signs in, and a redirect to /login would be cached in its place.
	r.Get("/offline.html", serve("offline.html", "text/html; charset=utf-8"))
}

func serve(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := fs.ReadFile(Files, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		// no-cache: a long-lived SW or manifest is how an install gets stuck
		// on a deleted icon or an old start_url.
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	}
}
