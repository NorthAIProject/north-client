// Command web serves the North web application.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/web/app"
	"github.com/NorthAIProject/north-client/web/assets"
	"github.com/NorthAIProject/north-client/web/landing"

	"github.com/a-h/templ"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet when configuration fails, so this one
		// message goes straight to stderr.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// run owns the process lifecycle. Keeping it separate from main means every
// exit path runs deferred cleanup, which os.Exit inside main would skip.
func run() error {
	// A missing .env is normal in production, where the environment is
	// populated by the platform.
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("connected to database")

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: routes(cfg, pool),

		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: it would cut off SSE streams mid-answer. Individual
		// handlers bound their own work with context deadlines instead.
		IdleTimeout: 120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", slog.String("addr", srv.Addr), slog.String("env", string(cfg.Env)))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Give in-flight requests a chance to finish rather than dropping them.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("server stopped")
	return nil
}

func routes(cfg *config.Config, pool *pgxpool.Pool) http.Handler {
	// Wiring happens once, here. Every dependency is constructed explicitly and
	// passed down, so the shape of the application is readable in one place
	// rather than discovered through package-level initialisation.
	userRepo := users.NewRepository(pool)
	userSvc := users.NewService(userRepo)

	sessions := auth.NewSessionStore(pool, cfg.SessionLifetime)
	authSvc := auth.NewService(userSvc, sessions)
	authMW := auth.NewMiddleware(sessions, cfg.Env.IsProduction())
	authHandler := auth.NewHandler(authSvc, authMW, "/app")

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(slog.Default()))
	r.Use(middleware.Recover)
	r.Use(middleware.CSRF(cfg.Env.IsProduction()))
	r.Use(authMW.LoadUser)

	mountAssets(r, cfg)

	r.Get("/healthz", healthz(pool))
	r.Method(http.MethodGet, "/", templ.Handler(landing.Page()))

	authHandler.Routes(r)

	// Everything under /app requires a session.
	r.Route("/app", func(r chi.Router) {
		r.Use(authMW.RequireAuth)

		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			user := auth.MustUser(req.Context())
			if err := app.Dashboard(user).Render(req.Context(), w); err != nil {
				middleware.FromContext(req.Context()).Error("render dashboard", slog.Any("error", err))
			}
		})
	})

	return r
}

// healthz reports whether the process can serve traffic. It checks the database
// because an instance that cannot reach Postgres should be taken out of a load
// balancer rather than left accepting requests it will fail.
func healthz(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")

		if err := pool.Ping(ctx); err != nil {
			middleware.FromContext(r.Context()).Error("health check failed", slog.Any("error", err))
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"status":"unhealthy","database":"unreachable"}`)
			return
		}

		fmt.Fprint(w, `{"status":"ok","database":"ok"}`)
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

	r.Handle("/assets/*", http.StripPrefix("/assets/", http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			if isProd {
				// Asset URLs are cache-busted by templUI's ScriptURL, so a long
				// max-age is safe.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-store")
			}
			fs.ServeHTTP(w, req)
		},
	)))
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}

	// JSON in production so log aggregation can parse it; text locally so a
	// human can read it.
	if cfg.Env.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
