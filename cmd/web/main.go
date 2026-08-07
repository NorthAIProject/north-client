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

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/care"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/fitness"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/settings"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
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

	if migrateErr := database.Migrate(ctx, cfg.DatabaseURL); migrateErr != nil {
		return fmt.Errorf("database migrations: %w", migrateErr)
	}
	log.Info("database migrations applied")

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("connected to database")

	registry, err := providers.Build(ctx, providers.Options{
		Default:           cfg.AI.Provider,
		Model:             cfg.AI.Model,
		GeminiAPIKey:      cfg.AI.GeminiAPIKey,
		OpenRouterAPIKey:  cfg.AI.OpenRouterAPIKey,
		OpenRouterSiteURL: cfg.AI.OpenRouterSiteURL,
		OpenRouterSiteApp: cfg.AI.OpenRouterSiteApp,
	})
	if err != nil {
		return err
	}

	log.Info("ai providers ready",
		slog.String("default", registry.DefaultName()),
		slog.Any("registered", registry.Names()),
	)

	storage, err := media.NewS3Storage(ctx, media.S3Options{
		Endpoint:     cfg.Storage.Endpoint,
		Region:       cfg.Storage.Region,
		Bucket:       cfg.Storage.Bucket,
		AccessKey:    cfg.Storage.AccessKey,
		SecretKey:    cfg.Storage.SecretKey,
		UsePathStyle: cfg.Storage.UsePathStyle,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: routes(cfg, pool, registry, storage),

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

func routes(cfg *config.Config, pool *pgxpool.Pool, registry *ai.Registry, storage media.Storage) http.Handler {
	// Wiring happens once, here. Every dependency is constructed explicitly and
	// passed down, so the shape of the application is readable in one place
	// rather than discovered through package-level initialisation.
	userRepo := users.NewRepository(pool)
	userSvc := users.NewService(userRepo)

	sessions := auth.NewSessionStore(pool, cfg.SessionLifetime)
	authSvc := auth.NewService(userSvc, sessions, auth.ServiceOptions{
		BaseURL: cfg.BaseURL,
		// LogMailer until a real SMTP provider is wired; reset links show in logs.
		Mailer:              auth.LogMailer{},
		GoogleClientID:      cfg.GoogleClientID,
		GoogleClientSecret:  cfg.GoogleClientSecret,
		WebAuthnRPID:        cfg.WebAuthnRPID,
		WebAuthnDisplayName: cfg.WebAuthnDisplayName,
		Log:                 slog.Default(),
	})
	authMW := auth.NewMiddleware(sessions, cfg.Env.IsProduction())
	authHandler := auth.NewHandler(authSvc, authMW, "/app")

	conversationSvc := conversations.NewService(conversations.NewRepository(pool))
	queue := jobs.NewQueue(pool)

	goalSvc := goals.NewService(goals.NewRepository(pool))
	goalHandler := goals.NewHandler(goalSvc)

	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	checkinHandler := checkins.NewHandler(checkinSvc, goalSvc)

	memorySvc := memories.NewService(memories.NewRepository(pool))
	memoryHandler := memories.NewHandler(memorySvc)

	workoutSvc := workouts.NewService(workouts.Options{
		Repository: workouts.NewRepository(pool),
		Registry:   registry,
		Model:      cfg.AI.Model,
	})
	workoutHandler := workouts.NewHandler(workoutSvc)

	mediaSvc := media.NewService(media.Options{
		Repository: media.NewRepository(pool),
		Storage:    storage,
		Queue:      queue,
		Registry:   registry,
		Model:      cfg.AI.Model,
	})
	mediaHandler := media.NewHandler(mediaSvc)

	// Biometrics -> calculator/activity both need the user's current weight,
	// so biometrics is constructed first and passed in as a lookup rather
	// than each depending on its concrete type.
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))

	calculatorSvc := calculator.NewService(calculator.NewRepository(pool), biometricSvc)

	activitySvc := activity.NewService(activity.NewRepository(pool), biometricSvc)
	activityHandler := activity.NewHandler(activitySvc)

	calculatorHandler := calculator.NewHandler(calculatorSvc, biometricSvc)

	fitnessHandler := fitness.NewHandler()

	mealsRepo := meals.NewRepository(pool)
	mealIngredientSvc := meals.NewIngredientService(mealsRepo)
	mealDietSvc := meals.NewDietPreferenceService(mealsRepo)
	mealPlanSvc := meals.NewMealPlanService(mealsRepo)
	foodLogSvc := meals.NewFoodLogService(mealsRepo)
	mealProgressSvc := meals.NewTrackMealProgressService(foodLogSvc, calculatorSvc)
	mealRecommendSvc := meals.NewGoalRecommendationService(mealProgressSvc, calculatorSvc)
	mealReminderSvc := meals.NewMealReminderService(mealsRepo)
	mealsHandler := meals.NewHandler(meals.HandlerOptions{
		Ingredients: mealIngredientSvc,
		Diets:       mealDietSvc,
		Plans:       mealPlanSvc,
		FoodLog:     foodLogSvc,
		Progress:    mealProgressSvc,
		Recommend:   mealRecommendSvc,
	})

	preferencesSvc := preferences.NewService(preferences.NewRepository(pool))
	settingsHandler := settings.NewHandler(userSvc, preferencesSvc, mealDietSvc)

	mindSvc := mind.NewService(mind.NewRepository(pool), checkinSvc)
	mindHandler := mind.NewHandler(mindSvc)

	careHandler := care.NewHandler(mealReminderSvc, checkinSvc)

	coachSvc := coach.NewService(coach.Options{
		Registry:      registry,
		Conversations: conversationSvc,
		// Context sources are registered here and nowhere else. Goals,
		// check-ins, memories, and knowledge search each add one as their
		// slices are built; the ContextBuilder itself never changes.
		ContextBuilder: coach.NewContextBuilder(conversationSvc,
			goals.NewContextSource(goalSvc),
			checkins.NewContextSource(checkinSvc),
			memories.NewContextSource(memorySvc),
			workouts.NewContextSource(workoutSvc),
			media.NewContextSource(mediaSvc),
			calculator.NewContextSource(calculatorSvc),
			activity.NewContextSource(activitySvc),
			meals.NewContextSource(mealProgressSvc, mealDietSvc),
			preferences.NewContextSource(preferencesSvc),
			mind.NewContextSource(mindSvc),
		),
		PromptBuilder: coach.NewPromptBuilder(),
		Queue:         queue,
		Model:         cfg.AI.Model,
		FastModel:     cfg.AI.FastModel,
	})
	coachHandler := coach.NewHandler(coachSvc)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(slog.Default()))
	r.Use(middleware.Recover)
	// Before CSRF: that middleware parses multipart bodies to find the token,
	// so the cap has to be in place first. Slightly above the media limit, so a
	// too-large video gets the media handler's explanation rather than a bare
	// connection error.
	r.Use(middleware.MaxBody(media.MaxVideoBytes + (16 << 20)))
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
			data := app.DashboardData{}
			if _, err := checkinSvc.Today(req.Context(), user); err == nil {
				data.CheckedInToday = true
			}
			if n, err := checkinSvc.Streak(req.Context(), user); err == nil {
				data.Streak = n
			}
			if n, err := memorySvc.CountPending(req.Context(), user.ID); err == nil {
				data.PendingMemories = n
			}
			if err := app.Dashboard(user, data).Render(req.Context(), w); err != nil {
				middleware.FromContext(req.Context()).Error("render dashboard", slog.Any("error", err))
			}
		})

		coachHandler.Routes(r)
		checkinHandler.Routes(r)
		goalHandler.Routes(r)
		memoryHandler.Routes(r)
		workoutHandler.Routes(r)
		mediaHandler.Routes(r)
		settingsHandler.Routes(r)
		mindHandler.Routes(r)
		careHandler.Routes(r)
		activityHandler.Routes(r)
		calculatorHandler.Routes(r)
		mealsHandler.Routes(r)
		fitnessHandler.Routes(r)
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
