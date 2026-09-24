package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/posthog/posthog-go"

	"github.com/NorthAIProject/north-client/internal/account"
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/agent"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/capture"
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
	"github.com/NorthAIProject/north-client/internal/integrations"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/mcpauth"
	"github.com/NorthAIProject/north-client/internal/mcpserver"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/messaging/telegram"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/news"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/push"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/settings"
	"github.com/NorthAIProject/north-client/internal/shared/metrics"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/toolaudit"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/vault"
	vaultdb "github.com/NorthAIProject/north-client/internal/vault/db"
	"github.com/NorthAIProject/north-client/internal/voice"
	"github.com/NorthAIProject/north-client/internal/voice/vocab"
	"github.com/NorthAIProject/north-client/internal/watches"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/web/assets"
	"github.com/NorthAIProject/north-client/web/landing"
	"github.com/NorthAIProject/north-client/web/legal"
	"github.com/NorthAIProject/north-client/web/pwa"
	"github.com/a-h/templ"
)

func routes(
	cfg *config.Config,
	pool *pgxpool.Pool,
	registry *ai.Registry,
	storage media.Storage,
	embedder ai.Embedder,
	posthogClient posthog.Client,
	metricsReg *metrics.Registry,
) (http.Handler, background) {
	// Wiring happens once, here. Every dependency is constructed explicitly and
	// passed down, so the shape of the application is readable in one place
	// rather than discovered through package-level initialisation.

	// One runner for the process. Everything that calls a model goes through
	// it, so a provider that is out of credit or overloaded costs a fallback
	// rather than a failed request.
	runner := ai.NewRunner(registry, cfg.AI.ChainSet())

	// One funnel for the process. Every event below flows through it, and a
	// deployment with no PostHog key makes each call a no-op.
	funnel := analytics.New(posthogClient)

	userRepo := users.NewRepository(pool)
	userSvc := users.NewService(userRepo)

	sessions := auth.NewSessionStore(pool, cfg.SessionLifetime)
	authSvc := auth.NewService(userSvc, sessions, auth.ServiceOptions{
		BaseURL: cfg.BaseURL,
		// A real mailer when one is configured, the log otherwise. Nothing else
		// has to change: Service.PasswordResetEnabled turns the reset journey
		// back on by itself once delivery stops being a log line.
		Mailer:              mailer(cfg),
		Production:          cfg.Env.IsProduction(),
		GoogleClientID:      cfg.GoogleClientID,
		GoogleClientSecret:  cfg.GoogleClientSecret,
		GoogleIOSClientID:   cfg.GoogleIOSClientID,
		AppleBundleID:       cfg.AppleBundleID,
		WebAuthnRPID:        cfg.WebAuthnRPID,
		WebAuthnDisplayName: cfg.WebAuthnDisplayName,
		Log:                 slog.Default(),
	}).WithFunnel(funnel)
	authMW := auth.NewMiddleware(sessions, cfg.Env.IsProduction(), cfg.TrustedProxies)
	authThrottle := auth.NewThrottle(auth.ThrottleConfig{
		PerMinute:         cfg.AuthAttemptsPerMinute,
		PerEmailPerMinute: cfg.AuthAttemptsPerEmailPerMinute,
		TrustedProxies:    cfg.TrustedProxies,
	}, slog.Default())
	authHandler := auth.NewHandler(authSvc, authMW, "/app", authThrottle)

	conversationSvc := conversations.NewService(conversations.NewRepository(pool))

	// A second handle on the generations table, read-only. run() owns the one
	// the meter writes through; this one only ever answers "what did this
	// account spend", for the insights section.
	spendRepo := spend.NewRepository(pool)
	queue := jobs.NewQueue(pool)

	goalSvc := goals.NewService(goals.NewRepository(pool))
	goalHandler := goals.NewHandler(goalSvc).WithFunnel(funnel)

	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	checkinHandler := checkins.NewHandler(checkinSvc, goalSvc).WithFunnel(funnel)

	notificationSvc := notifications.NewService(notifications.NewRepository(pool))

	// Web Push to the browsers a person subscribed. Built whether or not keys
	// exist: without them Enabled is false, the settings page shows no row, the
	// dashboard offers no step, and Send delivers to nobody.
	vapid := push.VAPIDFrom(cfg.Push)
	pushSvc := push.NewService(push.NewRepository(pool), push.NewSender(vapid), vapid, slog.Default()).
		WithFunnel(funnel)
	pushHandler := push.NewHandler(pushSvc)

	nudgeSvc := nudges.NewService(nudges.NewRepository(pool), userSvc, checkinSvc, goalSvc).
		WithPrefs(notificationSvc).
		WithPush(pushSvc).
		WithFunnel(funnel)
	nudgeHandler := nudges.NewHandler(nudgeSvc)

	memorySvc := memories.NewService(memories.NewRepository(pool))
	memoryHandler := memories.NewHandler(memorySvc)

	// Notes and uploaded documents. Bytes go to the same object storage as
	// media; parsing and chunking happen on the worker, never here.
	documentSvc := documents.NewService(documents.NewRepository(pool), storage, queue).
		WithFunnel(funnel)

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
		cfg.QuotaLimits(),
		func(ctx context.Context) (quota.Identity, bool) {
			user, ok := auth.UserFrom(ctx)
			return quota.Identity{UserID: user.ID, Tier: string(user.Tier)}, ok
		},
	)

	documentHandler := documents.NewHandler(documentSvc, quotaSvc)

	vaultSvc := vault.NewService(vault.Options{
		Repository: vault.NewRepository(vaultdb.New(pool)),
		Documents:  documentSvc,
		Queue:      queue,
	})
	vaultHandler := vault.NewHandler(vaultSvc, !cfg.Env.IsProduction())

	// account and export are the two halves of the same promise — leaving with
	// your data, and being able to leave at all — so they are built together and
	// share the record of both having happened.
	accountSvc := account.NewService(account.NewRepository(pool), storage, slog.Default())

	// Export reads across profile, goals, check-ins, memories, documents and
	// conversations, which is why it is its own package rather than a method on
	// any one of them.
	exportHandler := export.NewHandler(export.NewExporter(export.Options{
		Documents:     documentSvc,
		Memories:      memorySvc,
		Conversations: conversationSvc,
		Goals:         goalSvc,
		CheckIns:      checkinSvc,
		Storage:       storage,
	}), quotaSvc, accountSvc)

	// Built before workouts: the plan generator picks from this catalog, so
	// the catalog has to exist before the thing that reads it.
	exerciseSvc := exercises.NewService(exercises.NewRepository(pool))
	exerciseHandler := exercises.NewHandler(exerciseSvc)

	workoutSvc := workouts.NewService(workouts.Options{
		Repository: workouts.NewRepository(pool),
		Runner:     runner,
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

	// Breaking-news ticker. Off entirely when FEEDS_BASE_URL is unset: no
	// strip, no settings routes, and the dashboard never asks.
	var newsSvc *news.Service
	if cfg.News.Enabled() {
		newsSvc = news.NewService(news.NewRepository(pool), news.NewClient(cfg.News.FeedsBaseURL, nil), preferencesSvc, cfg.News.CuratedFeeds)
	}

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
	}).WithFunnel(funnel)

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
	mealsOpts := meals.HandlerOptions{
		Ingredients: mealIngredientSvc,
		Diets:       mealDietSvc,
		Plans:       mealPlanSvc,
		FoodLog:     foodLogSvc,
		Progress:    mealProgressSvc,
		Recommend:   mealRecommendSvc,
	}
	mealsHandler := meals.NewHandler(mealsOpts)

	// Personal access tokens for outside agents. The base URL comes from
	// configuration and not from the request, because the setup instructions
	// this renders carry a live credential and a host-derived URL would let a
	// visitor decide where it is sent.
	connectionSvc := connections.NewService(connections.NewRepository(pool), userSvc, cfg.BaseURL)

	// Bring-your-own-key, when the deployment has somewhere safe to put one.
	// Without ENCRYPTION_KEY the sealer is nil and the feature reports itself
	// unavailable, rather than storing somebody's credential in the clear.
	// A second meter over the same pool rather than one threaded through
	// routes(): it is a repository handle, not a resource, and the alternative
	// is another parameter on a function that already takes seven.
	aicredSvc := aicreds.NewService(aicreds.NewRepository(pool), sealer, slog.Default())
	aicredSvc = aicredSvc.WithToolProbe(aicreds.NewToolProbe()).
		WithMeter(spend.NewMeter(spend.NewRepository(pool)))

	// North as an MCP *client*: the calendar somebody connected, reached over
	// somebody else's server. The opposite direction from the /mcp route below,
	// which is North serving its own tools to an agent.
	integrationSvc := integrations.NewService(
		integrations.NewRepository(pool, sealer),
		integrations.NewCalendarAdapter(integrations.NewClient()),
	).WithFunnel(funnel)

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

	careOpts := care.Options{
		Reminders: mealReminderSvc,
		CheckIns:  checkinSvc,
		Hydration: hydrationSvc,
		Sleep:     sleepSvc,
		Habits:    habitSvc,
	}
	careHandler := care.NewHandler(careOpts)

	// Speech to text: a dedicated endpoint, not a chat model.
	//
	// The cluster runs its own, which is free per request and keeps the audio
	// inside it. There is no chain and no failover here on purpose — the
	// alternative to this service is silence, not a guess, and a chat provider
	// that cannot actually hear answers the prompt instead of the recording.
	// That answer would then be stored as the user's own words.
	//
	// No base URL means no transcriber, which means voice is off and says so in
	// words on both surfaces. Every typed path is untouched.
	var transcriber ai.Transcriber
	if cfg.Transcription.Enabled() {
		client, err := openaicompat.NewTranscriptionClient(openaicompat.TranscriptionOptions{
			BaseURL:      cfg.Transcription.BaseURL,
			APIKey:       cfg.Transcription.APIKey,
			Model:        cfg.Transcription.Model,
			EnglishModel: cfg.Transcription.EnglishModel,
		})
		if err != nil {
			// A misconfigured endpoint is a boot failure rather than something
			// the first person to hold the microphone discovers.
			panic(err)
		}
		transcriber = client
	} else {
		slog.Warn("no transcription endpoint configured; voice notes will be refused in words")
	}

	// Voice is built once and shared. Two surfaces accept speech — the web
	// recorder and, further down, Telegram voice notes — and the bounds and
	// refusals that go with a recording should not be written twice.
	voiceSvc := voice.NewService(voice.Options{
		Transcriber: transcriber,

		// The recogniser mangles proper nouns, and the cheapest correction
		// available is telling it which ones this account already contains —
		// goal titles and habit names. The infra doc calls this the highest
		// leverage thing in the feature, and it is two indexed queries against
		// a transcription that takes seconds.
		Vocabulary: vocab.New(goalSvc, habitSvc),
	}).WithMetrics(metricsReg)

	// Dictation: the microphone beside every text box that reaches the coach.
	// It answers with words for the box and nothing else — the person still
	// sends what they said, the same way they would have after typing.
	voiceHandler := voice.NewHandler(voiceSvc, quotaSvc)

	// Quick capture composes the six logging slices behind one box. It owns no
	// table; the parse is a model call and the commit is the same writes the
	// care page makes.
	captureHandler := capture.NewHandler(capture.NewService(capture.Options{
		// FastModel for the same reason the daily briefing uses it: this is
		// transcription, not writing.
		Parser: capture.NewAIParser(runner, cfg.AI.FastModel),

		Hydration:   hydrationSvc,
		Sleep:       sleepSvc,
		Habits:      habitSvc,
		Biometrics:  biometricSvc,
		FoodLog:     foodLogSvc,
		Ingredients: mealIngredientSvc,
		CheckIns:    checkinSvc,
	}), quotaSvc)

	captureAPI := capture.NewAPI(captureHandler.Service(), connectionSvc, quotaSvc, slog.Default())
	authAPI := auth.NewAPI(sessions).WithAuthService(authSvc, authMW)

	dashboardOpts := dashboard.Options{
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
		Push:          pushSvc,
	}
	if newsSvc != nil {
		dashboardOpts.NewsTicker = newsSvc
	}
	dashboardSvc := dashboard.NewService(dashboardOpts)
	dashboardHandler := dashboard.NewHandler(dashboardSvc)
	dashboardAPI := dashboard.NewAPI(dashboardSvc)

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

		Food:       foodLogSvc,
		MacroGoals: calculatorSvc,

		Conversations: conversationSvc,
		Spend:         spendRepo,
		Health:        healthSvc,
		SiteURL:       cfg.BaseURL,
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
		Client:     reports.ClientFromChain(runner),
		Context:    reports.NewInsightsContext(insightsSvc, mealProgressSvc, memorySvc),
		FastModel:  cfg.AI.FastModel,
	})
	reportHandler := reports.NewHandler(reportSvc, quotaSvc)

	// Late-wired: see Service.WithBriefings. reports needs insights, and
	// insights needs the dashboard, so the briefing card arrives last.
	dashboardSvc.WithBriefings(reportSvc)

	// One registry of capabilities, shared by the coach's chat loop and the
	// MCP server. Two definitions of "calculate my macros" would drift, and
	// the drift would show as the coach and Telegram disagreeing.
	// Standing tasks. The web process only creates them — create_watch runs
	// when somebody confirms the card; the worker's sweep runs them.
	watchSvc := watches.NewService(watches.NewRepository(pool), userSvc)

	agentTools := agent.Build(agent.Services{
		SiteURL:       cfg.BaseURL,
		Watches:       watchSvc,
		Exercises:     exerciseSvc,
		Calculator:    calculatorSvc,
		Goals:         goalSvc,
		Ingredients:   mealIngredientSvc,
		FoodLog:       foodLogSvc,
		CheckIns:      checkinSvc,
		Documents:     documentSvc,
		Workouts:      workoutSvc,
		Users:         userSvc,
		Notifications: notificationSvc,

		// The day's logs, through the slices that already own them.
		Hydration:  hydrationSvc,
		Sleep:      sleepSvc,
		Habits:     habitSvc,
		Biometrics: biometricSvc,
		Activity:   activitySvc,
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
			integrations.NewContextSource(integrationSvc),
		).WithMetrics(metricsReg),
		PromptBuilder:   coach.NewPromptBuilder(),
		Queue:           queue,
		Chains:          cfg.AI.ChainSet(),
		Tools:           agentTools,
		Declines:        auditRecorder,
		ExternalLookups: auditSvc,
		ExerciseLinks:   exerciseSvc,
		// Tried ahead of the chain above, so a user who supplied a key is
		// served by it and a user who did not is unaffected.
		Own:         aicredSvc,
		Analytics:   coach.NewAnalytics(posthogClient).WithMetrics(metricsReg),
		Funnel:      funnel,
		Attachments: mediaSvc,
		Model:       cfg.AI.Model,
		FastModel:   cfg.AI.FastModel,
	})
	coachHandler := coach.NewHandler(coachSvc, quotaSvc).WithImages(mediaSvc)

	// Wired after construction: the bell and the coach both already exist,
	// and a cycle of constructors would be worse than two setters.
	nudgeSvc.WithWeek(nudges.WeekFrom{
		Chats:  conversationSvc,
		Photos: mediaSvc,
		Facts:  memorySvc,
	}).WithTraining(workoutSvc).WithSchedules(notificationSvc)
	coachSvc.WithInbox(coach.InboxFunc(nudgeSvc.RaiseFromUser))
	mediaSvc.WithOnReady(func(ctx context.Context, userID, analysisID uuid.UUID) {
		_ = nudgeSvc.Note(ctx, userID, nudges.KindFormReady, analysisID.String(),
			"Your form check is ready",
			"I watched the clip. Open it to see the cues.",
			"/app/form/"+analysisID.String())
	})
	reportSvc.WithInbox(nudgeSvc).WithChats(conversationSvc)

	// Which Telegram edge runs follows from the configuration rather than from
	// a switch, so there is no combination that serves a webhook with no
	// secret. Both are nil without a bot token, and then neither the route nor
	// the poller exists. The client is built first so messaging can use it
	// to push briefings, not only to reply.
	var telegramWebhook *telegram.Webhook
	var telegramPoller *telegram.Poller
	var telegramClient *telegram.Client
	if cfg.Telegram.Enabled() {
		telegramClient = telegram.NewClient(cfg.Telegram.BotToken)
	}

	// The second mouth on the same brain. Built unconditionally because the
	// settings page needs it to issue link codes; whether anything can reach it
	// depends on a bot token, below.
	messagingOpts := messaging.Options{
		Art:     exerciseSvc,
		SiteURL: cfg.BaseURL,

		// Answers /stats from the same deterministic view the insights page
		// renders, so the two can never disagree.
		Stats:   insightsSvc,
		Funnel:  funnel,
		Coach:   coachSvc,
		Threads: conversationSvc,
		Users:   userSvc,
		Links:   messaging.NewRepository(pool),

		// The same budget the web chat spends, deliberately. A surface that
		// reached the coach without this would be a way around the limits
		// rather than a second way in — the gap ask_coach still has.
		Quotas: quotaSvc,
		Images: mediaSvc,

		// The same service the web recorder uses. A voice note becomes the
		// sentence the person would have typed, and the coach answers that —
		// so dictating and typing reach the same place, which is the whole
		// point of the feature.
		Voice: voiceSvc,

		Log: slog.Default(),
	}

	// Set here rather than in the literal above, and only when there is a
	// client, because a nil *telegram.Client assigned to an interface field is
	// not a nil interface: every `== nil` guard inside the service silently
	// stops working and the first call dereferences it. A deployment with no
	// bot token leaves both of these genuinely nil.
	//
	// Files is the same client, and it is what lets the service decide when a
	// recording is worth downloading rather than the adapter fetching every one
	// on arrival.
	if telegramClient != nil {
		messagingOpts.Transport = telegramClient
		messagingOpts.Files = telegramClient
	}

	messagingSvc := messaging.NewService(messagingOpts)
	nudgeSvc.WithFanout(messagingSvc)

	settingsHandler := settings.NewHandler(
		userSvc, preferencesSvc, notificationSvc, mealDietSvc, connectionSvc, aicredSvc,
		auditSvc, messagingSvc, cfg.Telegram, accountSvc, authMW,
	).WithIntegrations(integrationSvc).WithPush(pushSvc)

	// Given the coach so the questionnaire ends in a thread that is already
	// being answered, rather than an empty chat box the person has to think of
	// something to say to.
	onboardingSvc := onboarding.NewService(userSvc, memorySvc, goalSvc).
		WithCoach(coachSvc, slog.Default()).
		WithFunnel(funnel)
	onboardingHandler := onboarding.NewHandler(onboardingSvc)
	onboardingAPI := onboarding.NewAPI(onboardingSvc)

	if telegramClient != nil {
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

	// OAuth in front of /mcp, so connecting an agent is one pasted URL rather
	// than a token copied into a configuration file.
	//
	// Built before the endpoint because the endpoint's 401 has to point at this
	// server's discovery document: that pointer is how an unauthenticated
	// client bootstraps itself instead of simply failing.
	mcpAuthSvc := mcpauth.NewService(mcpauth.NewRepository(pool), connectionSvc, cfg.BaseURL).
		WithFunnel(funnel)
	mcpAuthMachine := mcpauth.NewMachineHandler(mcpAuthSvc, slog.Default(), cfg.TrustedProxies)

	// The consent screen. It creates accounts, so it holds the auth service and
	// the onboarding service rather than reimplementing either.
	mcpAuthBrowser := mcpauth.NewBrowserHandler(
		mcpAuthSvc, authSvc, authMW, onboardingSvc, slog.Default(), cfg.Env.IsProduction(),
	).WithFunnel(funnel)

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
		TrustedProxies:    cfg.TrustedProxies,
		Version:           mcpserver.Version,
		Log:               slog.Default(),

		Funnel: funnel,

		// What the 401 points at. cmd/mcp-server leaves this empty and keeps
		// the header it always had: a static token on a tailnet has no
		// authorization server to discover.
		ResourceMetadataURL: mcpAuthSvc.ResourceMetadataURL(),
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

	// OAuth's machine half sits beside /mcp for the same reasons and one more.
	//
	// A client fetching discovery has no session, a token request authenticates
	// with a code rather than a cookie, and both need permissive CORS so a
	// browser-based client can call them from its own page — which is the
	// opposite of what the consent screen needs. The consent screen is
	// therefore mounted in the session group further down, not here.
	//
	// A smaller cap than /mcp: everything here is a handful of short strings.
	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(64 << 10))
		mcpAuthMachine.Routes(r)
	})

	// /api/v1 sits beside /mcp for the same three reasons: no cookie, no form,
	// and a bearer a browser never attaches on its own. The native app signs
	// in for a bearer session; capture keeps its nk_ connection token. Body
	// caps are set per group inside mountAPI.
	r.Group(func(r chi.Router) {
		mountAPI(r, sessions, apiSet{
			auth:       authAPI,
			capture:    captureAPI,
			onboarding: onboardingAPI,
			dashboard:  dashboardAPI,
			coach:      coach.NewAPI(coachSvc, quotaSvc, mediaSvc),
			exercises:  exercises.NewAPI(exerciseSvc, assets.Assets),
			settings:   settings.NewAPI(settingsHandler),
			training:   workouts.NewAPI(workoutSvc),
			activity:   activity.NewAPI(activitySvc),
			health:     health.NewAPI(healthSvc),
			fitness:    fitness.NewAPI(stravaSvc, cfg.BaseURL),
			insights:   insights.NewAPI(insightsSvc),
			goals:      goals.NewAPI(goalSvc),
			checkins:   checkins.NewAPI(checkinSvc),
			reports:    reports.NewAPI(reportSvc),
			memories:   memories.NewAPI(memorySvc),
			knowledge:  documents.NewAPI(documentSvc, quotaSvc),
			formChecks: media.NewAPI(mediaSvc, quotaSvc),
			care:       care.NewAPI(careOpts),
			mind:       mind.NewAPI(mindSvc),
			nutrition:  meals.NewAPI(mealsOpts),
		})
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
			TrustedProxies: cfg.TrustedProxies,
			Log:            slog.Default(),
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
		// Locale before LoadUser, deliberately: this one can only guess from a
		// cookie or Accept-Language, and LoadUser overwrites the guess with the
		// account's own setting. The other order would let a laptop's browser
		// settings override what somebody chose in Khepri.
		r.Use(middleware.Locale)
		// Before LoadUser for the same reason as Locale: the snippet this feeds
		// is rendered for signed-out visitors too, and the landing page is the
		// one page whose numbers it exists to collect. LoadUser then adds the
		// account id on top, for the identify call.
		r.Use(middleware.Analytics(middleware.AnalyticsConfig{
			APIKey: cfg.PostHog.APIKey,
			Host:   cfg.PostHog.Host,
		}))
		r.Use(authMW.LoadUser)

		mountAssets(r, cfg)
		pwa.Mount(r)

		r.Get("/healthz", healthz(pool))
		r.Get("/.well-known/apple-app-site-association", auth.AppleAppSiteAssociation(cfg.AppleTeamID, cfg.AppleBundleID))

		// Internal operator endpoint: total user count for the portfolio
		// dashboard at facorreia.com/apps. Guarded by METRICS_SECRET.
		if cfg.MetricsSecret != "" {
			r.Get("/internal/metrics", metricsHandler(pool, cfg.MetricsSecret))
		}
		r.Method(http.MethodGet, "/", templ.Handler(landing.Page()))

		// Public and outside /app on purpose: somebody deciding whether to
		// trust the product with their health data has to be able to read the
		// policy before creating the account that would let them read it.
		r.Method(http.MethodGet, "/privacy", templ.Handler(legal.Privacy()))
		r.Method(http.MethodGet, "/terms", templ.Handler(legal.Terms()))

		// The footer language switcher, for visitors who have no account to
		// store a preference on. A POST rather than a link: it writes a cookie,
		// and a GET that changes state is a GET a prefetcher can fire.
		//
		// Signed-in users never see the switcher — Settings owns their language
		// — and this route would not help them if they found it, because
		// LoadUser overwrites the cookie with the account's own setting.
		r.Post("/locale", setLocale)

		authHandler.Routes(r)

		// The consent screen, inside this group rather than beside /mcp.
		//
		// It is a browser page: it reads the session cookie, resolves a locale,
		// and renders a form with a CSRF token. Its machine half — discovery,
		// registration, tokens — is mounted above, outside the group, because
		// those need permissive CORS and no cookie. Chi routes exact paths, so
		// splitting /oauth across the two groups is legal.
		mcpAuthBrowser.Routes(r)

		// Everything under /app requires a session.
		r.Route("/app", func(r chi.Router) {
			r.Use(authMW.RequireAuth)
			// Before any page renders, so each text box knows whether to offer
			// a microphone without every handler passing that along.
			r.Use(voiceHandler.Advertise)

			onboardingHandler.Routes(r)
			// Outside RequireOnboarded: the onboarding form offers dictation
			// too, for the coaching style written in the person's own words.
			voiceHandler.Routes(r)

			r.Group(func(r chi.Router) {
				r.Use(onboarding.RequireOnboarded)

				dashboardHandler.Routes(r)
				if newsSvc != nil {
					news.NewHandler(newsSvc).Routes(r)
				}
				insightsHandler.Routes(r)
				reportHandler.Routes(r)

				coachHandler.Routes(r)
				checkinHandler.Routes(r)
				nudgeHandler.Routes(r)
				pushHandler.Routes(r)
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
				captureHandler.Routes(r)
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
// setLocale remembers a visitor's language and sends them back where they were.
