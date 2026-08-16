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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/posthog/posthog-go"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/agent"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/care"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/decisions"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/export"
	"github.com/NorthAIProject/north-client/internal/fitness"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/mcpserver"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/messaging/telegram"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/settings"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/toolaudit"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/vault"
	vaultdb "github.com/NorthAIProject/north-client/internal/vault/db"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/web/assets"
	"github.com/NorthAIProject/north-client/web/landing"
	"github.com/NorthAIProject/north-client/web/pwa"

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

	registry, err := providers.Build(ctx, cfg.AI.ProviderOptions())
	if err != nil {
		return err
	}
	cfg.AI.LogReady(log, registry)

	// Said out loud at boot because the alternative is silence. A deployment
	// that forgot ENCRYPTION_KEY still starts and still works — it just cannot
	// offer bring-your-own-key, and it writes Strava tokens in the clear. Both
	// are invisible from the outside, so the only place anyone would find out
	// is here.
	if cfg.Encryption.Enabled() {
		log.Info("encryption at rest enabled", slog.Int("keys", len(cfg.Encryption.Keys)))
	} else {
		log.Warn("encryption at rest is not configured: " +
			"provider keys cannot be stored and Strava tokens are written in plaintext. Set ENCRYPTION_KEY")
	}

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

	// Semantic retrieval, when a provider is configured. A failure here is fatal
	// rather than silent: somebody who set EMBEDDING_PROVIDER has said what they
	// want, and booting with full-text-only retrieval would look like it worked
	// and surface as worse answers weeks later.
	embedder, err := providers.Embedder(registry, providers.EmbedderOptions{
		Provider:   cfg.Embedding.Provider,
		Model:      cfg.Embedding.Model,
		Dimensions: cfg.Embedding.Dimensions,
	})
	if err != nil {
		return err
	}

	posthogClient, err := newPostHogClient(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = posthogClient.Close() }()

	handler, runBackground := routes(cfg, pool, registry, storage, embedder, posthogClient)

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,

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

	// Background work stops with the signal context, not with the server, so a
	// poller mid-request finishes rather than being cut off by Shutdown. It is
	// logged rather than fatal: losing Telegram is not a reason to take the web
	// app down with it.
	if runBackground != nil {
		go func() {
			if err := runBackground(ctx); err != nil {
				log.Error("background worker stopped", slog.Any("error", err))
			}
		}()
	}

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

// background is work that outlives any one request.
//
// Today there is exactly one: the Telegram poller, which has to keep asking
// for updates for as long as the process runs. It is returned rather than
// started here because routes() has no lifecycle — no context to cancel, no
// place to report a failure — and a goroutine leaked out of a constructor is
// how a process ends up with two pollers after a test.
type background func(ctx context.Context) error

func routes(
	cfg *config.Config,
	pool *pgxpool.Pool,
	registry *ai.Registry,
	storage media.Storage,
	embedder ai.Embedder,
	posthogClient posthog.Client,
) (http.Handler, background) {
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

	nudgeSvc := nudges.NewService(nudges.NewRepository(pool), userSvc, checkinSvc, goalSvc)
	nudgeHandler := nudges.NewHandler(nudgeSvc)

	memorySvc := memories.NewService(memories.NewRepository(pool))
	memoryHandler := memories.NewHandler(memorySvc)

	// Notes and uploaded documents. Bytes go to the same object storage as
	// media; parsing and chunking happen on the worker, never here.
	documentSvc := documents.NewService(documents.NewRepository(pool), storage, queue)

	if embedder != nil {
		documentSvc = documentSvc.WithEmbeddings(embedder, slog.Default())
	}
	// One quota service for every guarded surface, so the budgets live in one
	// table and one place in the configuration rather than one per handler.
	//
	// The identity function is passed in rather than imported inside the
	// package: quota counts, it does not decide who is signed in.
	quotaSvc := quota.NewService(
		quota.NewRepository(pool),
		map[quota.Action]quota.Limit{
			quota.CoachMessage:    {PerWindow: cfg.Quota.CoachMessages, Window: time.Hour},
			quota.DocumentUpload:  {PerWindow: cfg.Quota.DocumentUploads, Window: time.Hour},
			quota.DocumentReindex: {PerWindow: cfg.Quota.DocumentReindexes, Window: time.Hour},
			quota.ReportGenerate:  {PerWindow: cfg.Quota.ReportGenerations, Window: time.Hour},
			quota.MediaAnalysis:   {PerWindow: cfg.Quota.MediaAnalyses, Window: time.Hour},
		},
		func(ctx context.Context) (uuid.UUID, bool) {
			user, ok := auth.UserFrom(ctx)
			return user.ID, ok
		},
	)

	documentHandler := documents.NewHandler(documentSvc, quotaSvc)

	vaultSvc := vault.NewService(vault.Options{
		Repository: vault.NewRepository(vaultdb.New(pool)),
		Documents:  documentSvc,
		Queue:      queue,
	})
	vaultHandler := vault.NewHandler(vaultSvc)

	// Export reads across memories, documents and conversations, which is why
	// it is its own package rather than a method on any one of them.
	exportHandler := export.NewHandler(export.NewExporter(documentSvc, memorySvc, conversationSvc, storage))

	// Built before workouts: the plan generator picks from this catalog, so
	// the catalog has to exist before the thing that reads it.
	exerciseSvc := exercises.NewService(exercises.NewRepository(pool))
	exerciseHandler := exercises.NewHandler(exerciseSvc)

	workoutSvc := workouts.NewService(workouts.Options{
		Repository: workouts.NewRepository(pool),
		Registry:   registry,
		Catalog:    exerciseSvc,
		Model:      cfg.AI.Model,
	})
	workoutHandler := workouts.NewHandler(workoutSvc)

	mediaSvc := media.NewService(media.Options{
		Repository: media.NewRepository(pool),
		Storage:    storage,
		Queue:      queue,
		Registry:   registry,
		Provider:   cfg.AI.UploadProvider,
		Model:      cfg.AI.Model,
	})
	mediaHandler := media.NewHandler(mediaSvc, quotaSvc)

	// Biometrics -> calculator/activity both need the user's current weight,
	// so biometrics is constructed first and passed in as a lookup rather
	// than each depending on its concrete type.
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))

	calculatorSvc := calculator.NewService(calculator.NewRepository(pool), biometricSvc)

	activitySvc := activity.NewService(activity.NewRepository(pool), biometricSvc)
	activityHandler := activity.NewHandler(activitySvc)

	// Preferences owns the units system, which the calculator renders in.
	preferencesSvc := preferences.NewService(preferences.NewRepository(pool))

	calculatorHandler := calculator.NewHandler(calculatorSvc, biometricSvc, preferencesSvc)

	// Built once and shared by everything that stores a user's secret: the
	// Strava tokens below, and the bring-your-own provider key further down.
	// Nil when no ENCRYPTION_KEY is configured, which each of them handles.
	sealer, err := cfg.Encryption.Sealer()
	if err != nil {
		// Unreachable: config.Load already validated these keys. Refusing
		// anyway, because the alternative to a sealer that failed to build is
		// one that silently is not there, and credentials would then be written
		// in the clear by a deployment that asked for encryption.
		panic("encryption keys passed validation but produced no sealer: " + err.Error())
	}

	// Strava is the first provider integration. Absent credentials leave it
	// reporting itself unconfigured rather than failing the boot, so a
	// developer without a Strava app can still run everything else.
	stravaSvc := strava.NewService(strava.Options{
		Repository:   strava.NewRepository(pool, sealer),
		Activity:     activitySvc,
		Biometrics:   biometricSvc,
		Queue:        queue,
		ClientID:     cfg.StravaClientID,
		ClientSecret: cfg.StravaClientSecret,
		BaseURL:      cfg.BaseURL,
	})

	mealsRepo := meals.NewRepository(pool)
	mealIngredientSvc := meals.NewIngredientService(mealsRepo)
	mealDietSvc := meals.NewDietPreferenceService(mealsRepo)
	mealPlanSvc := meals.NewMealPlanService(mealsRepo)
	foodLogSvc := meals.NewFoodLogService(mealsRepo)
	mealProgressSvc := meals.NewTrackMealProgressService(foodLogSvc, calculatorSvc)
	mealRecommendSvc := meals.NewGoalRecommendationService(mealProgressSvc, calculatorSvc)
	mealReminderSvc := meals.NewMealReminderService(mealsRepo)

	// Declared here rather than with the other lifestyle slices below because
	// the fitness hub reads it: readings a device pushed are part of what that
	// page is for.
	healthSvc := health.NewService(health.NewRepository(pool))
	// Lets one push carry finished workouts as well as readings. The activity
	// slice owns the dedupe and the calorie estimate, so a synced session is
	// costed exactly like a manually logged one.
	healthSvc.WithWorkouts(activitySvc, biometricSvc)

	fitnessHandler := fitness.NewHandler(fitness.Options{
		Activity: activitySvc,
		Workouts: workoutSvc,
		Strava:   stravaSvc,
		Meals:    mealProgressSvc,
		Health:   healthSvc,
	}, cfg.Env.IsProduction())
	mealsHandler := meals.NewHandler(meals.HandlerOptions{
		Ingredients: mealIngredientSvc,
		Diets:       mealDietSvc,
		Plans:       mealPlanSvc,
		FoodLog:     foodLogSvc,
		Progress:    mealProgressSvc,
		Recommend:   mealRecommendSvc,
	})

	// Personal access tokens for outside agents. The base URL comes from
	// configuration and not from the request, because the setup instructions
	// this renders carry a live credential and a host-derived URL would let a
	// visitor decide where it is sent.
	connectionSvc := connections.NewService(connections.NewRepository(pool), userSvc, cfg.BaseURL)

	// Bring-your-own-key, when the deployment has somewhere safe to put one.
	// Without ENCRYPTION_KEY the sealer is nil and the feature reports itself
	// unavailable, rather than storing somebody's credential in the clear.
	aicredSvc := aicreds.NewService(aicreds.NewRepository(pool), sealer, slog.Default())

	// One account of what North has done, kept by both surfaces: the registry
	// reports every capability it runs, and the coach reports the writes people
	// refuse — those never reach the registry at all.
	auditSvc := toolaudit.NewService(toolaudit.NewRepository(pool))
	auditRecorder := toolaudit.NewRecorder(auditSvc)

	// settingsHandler is built further down, once the messaging service exists:
	// the connections page is where a Telegram link begins, so it needs to be
	// able to issue a code.

	mindSvc := mind.NewService(mind.NewRepository(pool), checkinSvc)
	mindHandler := mind.NewHandler(mindSvc, checkinSvc)

	decisionSvc := decisions.NewService(decisions.NewRepository(pool))
	decisionHandler := decisions.NewHandler(decisionSvc)

	// Daily lifestyle signals. None of these own a page: they are logged from
	// /app/care, the same way biometrics and preferences are reached through
	// calculator and settings.
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))
	sleepSvc := sleep.NewService(sleep.NewRepository(pool))
	habitSvc := habits.NewService(habits.NewRepository(pool))

	careHandler := care.NewHandler(care.Options{
		Reminders: mealReminderSvc,
		CheckIns:  checkinSvc,
		Hydration: hydrationSvc,
		Sleep:     sleepSvc,
		Habits:    habitSvc,
	})

	dashboardSvc := dashboard.NewService(dashboard.Options{
		CheckIns:      checkinSvc,
		Goals:         goalSvc,
		Conversations: conversationSvc,
		Workouts:      workoutSvc,
		Memories:      memorySvc,
		Habits:        habitSvc,
		Hydration:     hydrationSvc,
		Sleep:         sleepSvc,
		Activity:      activitySvc,
		Mind:          mindSvc,
		Nudges:        nudgeSvc,
	})
	dashboardHandler := dashboard.NewHandler(dashboardSvc)

	// Insights reuses the dashboard's timeline rather than reimplementing the
	// merge across eight slices. Two copies of that would drift.
	insightsSvc := insights.NewService(insights.Options{
		Dashboard: dashboardSvc,
		CheckIns:  checkinSvc,
		Hydration: hydrationSvc,
		Sleep:     sleepSvc,
		Habits:    habitSvc,
		Goals:     goalSvc,
		Mind:      mindSvc,
		Activity:  activitySvc,
	})
	insightsHandler := insights.NewHandler(insightsSvc)

	// Built here rather than beside the other slices because a weekly review
	// reads the whole week through insights: it has to come after everything
	// insights itself depends on. Without Context the generator still runs and
	// still writes a report — one whose every section says "(none recorded)".
	reportSvc := reports.NewService(reports.Options{
		Repository: reports.NewRepository(pool),
		Users:      userSvc,
		Queue:      queue,
		Client:     reports.ClientFromRegistry(registry),
		Context:    reports.NewInsightsContext(insightsSvc, mealProgressSvc, memorySvc),
	})
	reportHandler := reports.NewHandler(reportSvc, quotaSvc)

	// One registry of capabilities, shared by the coach's chat loop and the
	// MCP server. Two definitions of "calculate my macros" would drift, and
	// the drift would show as the coach and Telegram disagreeing.
	agentTools := agent.Build(agent.Services{
		Exercises:   exerciseSvc,
		Calculator:  calculatorSvc,
		Goals:       goalSvc,
		Ingredients: mealIngredientSvc,
		FoodLog:     foodLogSvc,
		CheckIns:    checkinSvc,
		Documents:   documentSvc,
		Workouts:    workoutSvc,
		Users:       userSvc,
	})

	agentTools.Record(auditRecorder)

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
			documents.NewContextSource(documentSvc),
			workouts.NewContextSource(workoutSvc),
			media.NewContextSource(mediaSvc),
			calculator.NewContextSource(calculatorSvc),
			// Strava supplies distance and climb, which North's own sessions
			// do not carry. Nil would simply drop those from the summary.
			activity.NewContextSource(activitySvc, stravaSvc),
			meals.NewContextSource(mealProgressSvc, mealDietSvc),
			preferences.NewContextSource(preferencesSvc),
			mind.NewContextSource(mindSvc),
			decisions.NewContextSource(decisionSvc),
			hydration.NewContextSource(hydrationSvc),
			sleep.NewContextSource(sleepSvc),
			// nil clock: the real one. Shares DailySignals with sleep and
			// hydration, because a device's resting numbers are read the same
			// way — as background, before anything else is interpreted.
			health.NewContextSource(healthSvc, nil),
			habits.NewContextSource(habitSvc),
			reports.NewContextSource(reportSvc),
		),
		PromptBuilder: coach.NewPromptBuilder(),
		Queue:         queue,
		Chains:        cfg.AI.ChainSet(),
		Tools:         agentTools,
		Declines:      auditRecorder,
		// Tried ahead of the chain above, so a user who supplied a key is
		// served by it and a user who did not is unaffected.
		Own:       aicredSvc,
		Analytics: coach.NewAnalytics(posthogClient),
		Model:     cfg.AI.Model,
		FastModel: cfg.AI.FastModel,
	})
	coachHandler := coach.NewHandler(coachSvc, quotaSvc)

	// The second mouth on the same brain. Built unconditionally because the
	// settings page needs it to issue link codes; whether anything can reach it
	// depends on a bot token, below.
	messagingSvc := messaging.NewService(messaging.Options{
		Coach:   coachSvc,
		Threads: conversationSvc,
		Users:   userSvc,
		Links:   messaging.NewRepository(pool),

		// The same budget the web chat spends, deliberately. A surface that
		// reached the coach without this would be a way around the limits
		// rather than a second way in — the gap ask_coach still has.
		Quotas: quotaSvc,
		Log:    slog.Default(),
	})

	settingsHandler := settings.NewHandler(
		userSvc, preferencesSvc, mealDietSvc, connectionSvc, aicredSvc,
		auditSvc, messagingSvc, cfg.Telegram,
	)

	// Given the coach so the questionnaire ends in a thread that is already
	// being answered, rather than an empty chat box the person has to think of
	// something to say to.
	onboardingSvc := onboarding.NewService(userSvc, memorySvc, goalSvc).
		WithCoach(coachSvc, slog.Default())
	onboardingHandler := onboarding.NewHandler(onboardingSvc)

	// Which Telegram edge runs follows from the configuration rather than from
	// a switch, so there is no combination that serves a webhook with no
	// secret. Both are nil without a bot token, and then neither the route nor
	// the poller exists.
	var telegramWebhook *telegram.Webhook
	var telegramPoller *telegram.Poller
	var telegramClient *telegram.Client
	if cfg.Telegram.Enabled() {
		telegramClient = telegram.NewClient(cfg.Telegram.BotToken)
		if cfg.Telegram.UsesWebhook() {
			telegramWebhook = telegram.NewWebhook(telegram.WebhookConfig{
				Messages: messagingSvc,
				Client:   telegramClient,
				Secret:   cfg.Telegram.WebhookSecret,
				Log:      slog.Default(),
			})
		} else {
			telegramPoller = telegram.NewPoller(telegram.PollerConfig{
				Messages: messagingSvc,
				Client:   telegramClient,
				Log:      slog.Default(),
			})
		}
	}

	// The MCP endpoint an outside agent connects to.
	//
	// Every token resolves to its own owner, which is what makes this safe to
	// serve publicly at all. Compare cmd/mcp-server, where one static token maps
	// to one configured account and the endpoint belongs on a tailnet.
	mcpEndpoint := mcpserver.Endpoint(mcpserver.Config{
		Services: mcpserver.Services{
			Users:     userSvc,
			Goals:     goalSvc,
			CheckIns:  checkinSvc,
			Memories:  memorySvc,
			Documents: documentSvc,
			Activity:  activitySvc,
			Coach:     coachSvc,
			Agent:     agentTools,
		},
		Auth: connectionSvc,

		// Empty unless MCP_ALLOWED_ORIGINS says otherwise, which rejects every
		// browser. Real MCP clients send no Origin; a request that does is a web
		// page, and a web page is not the intended caller.
		AllowedOrigins:    cfg.MCPAllowedOrigins,
		RequestsPerMinute: cfg.MCPRequestsPerMinute,
		Version:           mcpserver.Version,
		Log:               slog.Default(),
	})

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(slog.Default()))
	r.Use(middleware.Recover)

	// /mcp sits outside the session group, and the two middlewares it skips are
	// the reason it needs its own.
	//
	// CSRF is a browser defence: it works by requiring back a token the server
	// put in a form. An MCP client has no form and no cookie — it authenticates
	// with a bearer, which a browser never attaches on its own, so there is no
	// ambient authority to confuse and nothing for CSRF to protect. Left in the
	// main group it would reject every call with a 403 and an HTML body no MCP
	// client can read. LoadUser would meanwhile resolve a session that has
	// nothing to do with the token.
	//
	// The body cap is its own and far below the media limit: an MCP request is a
	// small JSON-RPC envelope, and there is no reason to accept a video's worth
	// of one.
	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(1 << 20))
		r.Handle("/mcp", mcpEndpoint)
	})

	// Health ingest sits beside /mcp for exactly the reasons above: the caller is
	// a background process on somebody's phone, holding the same revocable
	// bearer token and carrying neither a cookie nor a CSRF token.
	//
	// It gets its own group only because of the cap. An MCP call is a small
	// envelope; one health sync is a week of per-beat samples, and 1 MiB would
	// reject an ordinary Monday morning. This bound and health's own
	// maxReadings are two spellings of the same limit — a payload at the row
	// count lands near this size.
	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(8 << 20))
		r.Mount("/ingest/health", http.StripPrefix("/ingest/health", health.NewHandler(health.HandlerConfig{
			Service: healthSvc,
			Auth:    connectionSvc,

			// Left at the package defaults. The MCP bound is configurable
			// because a call there can reach a paid model and an operator may
			// need to tighten it; a write here costs a transaction, so there is
			// nothing yet for a knob to protect against.
			Log: slog.Default(),
		})))
	})

	// The Telegram webhook joins /mcp and /ingest/health for the third time for
	// the same reason: Telegram is not a browser, holds no cookie, and proves
	// itself with a shared secret in a header instead. Its own cap because an
	// update is a small JSON envelope and nothing legitimate approaches even
	// this.
	if telegramWebhook != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.MaxBody(1 << 20))
			r.Handle("/webhooks/telegram", telegramWebhook)
		})
	}

	r.Group(func(r chi.Router) {
		// Before CSRF: that middleware parses multipart bodies to find the token,
		// so the cap has to be in place first. Slightly above the media limit, so a
		// too-large video gets the media handler's explanation rather than a bare
		// connection error.
		r.Use(middleware.MaxBody(media.MaxVideoBytes + (16 << 20)))
		r.Use(middleware.CSRF(cfg.Env.IsProduction()))
		r.Use(authMW.LoadUser)

		mountAssets(r, cfg)
		pwa.Mount(r)

		r.Get("/healthz", healthz(pool))
		r.Method(http.MethodGet, "/", templ.Handler(landing.Page()))

		authHandler.Routes(r)

		// Everything under /app requires a session.
		r.Route("/app", func(r chi.Router) {
			r.Use(authMW.RequireAuth)

			onboardingHandler.Routes(r)

			r.Group(func(r chi.Router) {
				r.Use(onboarding.RequireOnboarded)

				dashboardHandler.Routes(r)
				insightsHandler.Routes(r)
				reportHandler.Routes(r)

				coachHandler.Routes(r)
				checkinHandler.Routes(r)
				nudgeHandler.Routes(r)
				goalHandler.Routes(r)
				memoryHandler.Routes(r)
				documentHandler.Routes(r)
				exportHandler.Routes(r)
				exerciseHandler.Routes(r)
				workoutHandler.Routes(r)
				mediaHandler.Routes(r)
				settingsHandler.Routes(r)
				vaultHandler.Routes(r)
				mindHandler.Routes(r)
				decisionHandler.Routes(r)
				careHandler.Routes(r)
				activityHandler.Routes(r)
				calculatorHandler.Routes(r)
				mealsHandler.Routes(r)
				fitnessHandler.Routes(r)
			})
		})
	})

	if telegramClient == nil {
		return r, nil
	}

	// Both edges need a moment with a context: the command menu is published
	// once at boot, and the poller then runs for the life of the process. In
	// webhook mode there is no poller and this returns after registering.
	return r, func(ctx context.Context) error {
		if err := telegramClient.RegisterCommands(ctx); err != nil {
			// Cosmetic. The commands are matched from the message text and work
			// regardless; what is lost is the menu that advertises them.
			slog.Default().Warn("could not publish the telegram command menu", slog.Any("error", err))
		}
		if telegramPoller == nil {
			return nil
		}
		return telegramPoller.Run(ctx)
	}
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

// newPostHogClient builds the client the coach reports LLM calls through.
//
// An absent key is a silently missed dashboard, not a broken app, so it is
// never fatal — except in development, where a quiet gap in analytics is
// worth a loud failure at boot rather than a puzzled look at an empty
// project weeks later. Production gets posthog-go's own no-op client, which
// answers every call without sending anything.
func newPostHogClient(cfg *config.Config) (posthog.Client, error) {
	if cfg.PostHog.APIKey == "" {
		if !cfg.Env.IsProduction() {
			return nil, fmt.Errorf("POSTHOG_API_KEY variable required by PostHog is missing or un-configured, this causes events to be silently missed. This error stops appearing once POSTHOG_API_KEY is configured")
		}
		return posthog.NewWithConfig("", posthog.Config{})
	}

	return posthog.NewWithConfig(cfg.PostHog.APIKey, posthog.Config{
		Endpoint: cfg.PostHog.Host,
	})
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
