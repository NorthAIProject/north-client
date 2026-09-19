package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/web/assets"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The redirect target is validated by auth.SafeRedirect for the same reason the
// login form's is: a `next` this handler will follow is an open redirect
// otherwise, and a language switcher is a perfectly ordinary thing to put a
// crafted link behind.
func setLocale(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	i18n.SetCookie(w, r, string(users.ResolveLocale(r.PostFormValue("locale"))))

	next := r.PostFormValue("next")
	if !auth.SafeRedirect(next) {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func healthz(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")

		if err := pool.Ping(ctx); err != nil {
			middleware.FromContext(r.Context()).Error("health check failed", slog.Any("error", err))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(w, `{"status":"unhealthy","database":"unreachable"}`)
			return
		}

		_, _ = fmt.Fprint(w, `{"status":"ok","database":"ok"}`)
	}
}

// mountAssets serves CSS, fonts, and component JavaScript.
//
// In development the files are read from disk so a Tailwind rebuild is visible
// on refresh. In production they are served from the embedded filesystem, which
// keeps the binary self-contained.
//
// templUI's own SetupScriptRoutes is deliberately not called: the CLI rewrote
// the component script path to /assets/js, so the vendored files under
// web/assets/js are already covered by this handler.
func mountAssets(r chi.Router, cfg *config.Config) {
	isProd := cfg.Env.IsProduction()

	var fs http.Handler
	if isProd {
		fs = http.FileServer(http.FS(assets.Assets))
	} else {
		fs = http.FileServer(http.Dir("./web/assets"))
	}

	r.Get("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/assets/brand/favicon.svg", http.StatusMovedPermanently)
	})

	r.Handle("/assets/*", http.StripPrefix("/assets/", http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			if isProd {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-store")
			}
			serveExerciseFrame(w, req)
			fs.ServeHTTP(w, req)
		},
	)))
}

// exercisePrefix is the one asset tree stored pre-compressed.
const exercisePrefix = "exercises/"

// serveExerciseFrame rewrites a request for an exercise frame onto the .svg.gz
// actually on disk, and declares the encoding the browser will need to undo.
//
// The frames are 24.7 MB of SVG raw and 11 MB gzipped, and they are embedded
// into the binary, so storing them compressed is 14 MB off every build and
// every image layer. Nothing here mounts compression middleware, so it is also
// the only thing keeping them from going out uncompressed at ~28 KB each.
//
// Templates ask for the .svg. Keeping .gz out of the markup means the storage
// decision stays in this function: if these ever move to object storage or the
// server grows a compressor, no template changes.
//
// The rewrite is unconditional rather than guarded by an existence check —
// every frame under this prefix is gzipped, and a slug that does not exist
// should 404, which it does either way. Content-Type has to be set here
// because the path now ends in .gz, and http.ServeContent keeps a Content-Type
// the caller already set rather than sniffing one from the extension.
func serveExerciseFrame(w http.ResponseWriter, req *http.Request) {
	if !strings.HasPrefix(req.URL.Path, exercisePrefix) || !strings.HasSuffix(req.URL.Path, ".svg") {
		return
	}
	req.URL.Path += ".gz"
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Encoding", "gzip")
}
