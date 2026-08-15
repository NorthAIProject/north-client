// Command worker runs North's background jobs.
//
// A separate binary from the web server, sharing the same code. Video analysis
// takes minutes and holds a provider connection open; running it inside the web
// process would mean a deploy interrupts work in flight, and one slow job would
// compete with request handling for the same connection pool.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/vault"
	vaultdb "github.com/NorthAIProject/north-client/internal/vault/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
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

	registry, err := providers.Build(ctx, cfg.AI.ProviderOptions())
	if err != nil {
		return err
	}
	cfg.AI.LogReady(log, registry)

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

	queue := jobs.NewQueue(pool)

	mediaSvc := media.NewService(media.Options{
		Repository: media.NewRepository(pool),
		Storage:    storage,
		Queue:      queue,
		Registry:   registry,
		Provider:   cfg.AI.UploadProvider,
		Model:      cfg.AI.Model,
	})

	memoryExtract := &memories.ExtractionService{
		Memories:      memories.NewService(memories.NewRepository(pool)),
		Conversations: conversations.NewService(conversations.NewRepository(pool)),
		Extractor: &memories.AIExtractor{
			Registry: registry,
			Model:    cfg.AI.FastModel,
		},
		Log: log,
	}

	// The worker refreshes Strava tokens, so it needs the same key the web
	// process seals them with. A deployment that gave one process the key and
	// not the other would have syncs failing on rows they cannot decrypt.
	sealer, err := cfg.Encryption.Sealer()
	if err != nil {
		return err
	}

	// Strava syncs run here rather than in the request that triggered them,
	// so a slow or rate-limited provider never holds a page open.
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	activitySvc := activity.NewService(activity.NewRepository(pool), biometricSvc)
	stravaSvc := strava.NewService(strava.Options{
		Repository:   strava.NewRepository(pool, sealer),
		Activity:     activitySvc,
		Biometrics:   biometricSvc,
		Queue:        queue,
		ClientID:     cfg.StravaClientID,
		ClientSecret: cfg.StravaClientSecret,
		BaseURL:      cfg.BaseURL,
	})

	// Documents are parsed and chunked here rather than during the upload:
	// indexing one file is bounded work, rebuilding a library is not, and
	// neither belongs in the time somebody spends watching a spinner.
	documentRepo := documents.NewRepository(pool)
	documentIndexer := documents.NewIndexer(documentRepo, storage).WithEmbeddingQueue(queue)

	// Semantic retrieval is optional. With no provider configured this is nil,
	// the job does nothing, and retrieval stays full-text — which is a complete
	// feature, not a degraded one.
	embedClient, err := providers.Embedder(registry, providers.EmbedderOptions{
		Provider:   cfg.Embedding.Provider,
		Model:      cfg.Embedding.Model,
		Dimensions: cfg.Embedding.Dimensions,
	})
	if err != nil {
		return err
	}
	documentEmbedder := documents.NewEmbedder(documentRepo, embedClient, log)
	embedModel := ""
	if embedClient != nil {
		embedModel = embedClient.EmbedModel()
	}
	embedSweeper := documents.NewEmbedSweeper(documentRepo, queue, embedModel, log)

	vaultSvc := vault.NewService(vault.Options{
		Repository: vault.NewRepository(vaultdb.New(pool)),
		Documents:  documents.NewService(documentRepo, storage, queue),
		Queue:      queue,
	})

	worker := jobs.NewWorker(queue, log)
	worker.Register(jobs.KindAnalyzeFormVideo, mediaSvc.AnalyzeVideo)
	worker.Register(jobs.KindExtractMemories, memoryExtract.HandleExtractJob)
	worker.Register(jobs.KindSyncStrava, syncStravaHandler(stravaSvc))
	worker.Register(jobs.KindIndexDocument, documentIndexer.HandleIndexDocument)
	worker.Register(jobs.KindReindexUser, documentIndexer.HandleReindexUser)
	worker.Register(jobs.KindEmbedChunks, documentEmbedder.HandleEmbedJob)
	worker.Register(jobs.KindSweepEmbeddings, embedSweeper.HandleSweep)
	worker.Register(jobs.KindSyncVault, vaultSvc.HandleSyncJob)

	// The weekly review is generated here, so the week's records have to be
	// readable here. The web process wires the same loader for the same reason;
	// wiring it in only one of them would give a report whose content depended
	// on which process happened to pick the job up.
	//
	// Dashboard is deliberately nil: it backs insights.Timeline, which is the
	// activity feed on a page, and a review never asks for it. Building the
	// dashboard here would drag in workouts and conversations to serve a call
	// that is never made.
	goalSvc := goals.NewService(goals.NewRepository(pool))
	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	userSvc := users.NewService(users.NewRepository(pool))
	calculatorSvc := calculator.NewService(calculator.NewRepository(pool), biometricSvc)
	mealsRepo := meals.NewRepository(pool)

	reviewContext := reports.NewInsightsContext(
		insights.NewService(insights.Options{
			CheckIns:  checkinSvc,
			Hydration: hydration.NewService(hydration.NewRepository(pool)),
			Sleep:     sleep.NewService(sleep.NewRepository(pool)),
			Habits:    habits.NewService(habits.NewRepository(pool)),
			Goals:     goalSvc,
			Mind:      mind.NewService(mind.NewRepository(pool), checkinSvc),
			Activity:  activitySvc,
		}),
		meals.NewTrackMealProgressService(meals.NewFoodLogService(mealsRepo), calculatorSvc),
		memories.NewService(memories.NewRepository(pool)),
	)

	reportSvc := reports.NewService(reports.Options{
		Repository: reports.NewRepository(pool),
		Users:      userSvc,
		Queue:      queue,
		Client:     reports.ClientFromRegistry(registry),
		Context:    reviewContext,
	})
	worker.Register(jobs.KindWeeklyReview, reportSvc.HandleGenerateJob)

	// The coach enqueues extraction only once a thread reaches four messages,
	// from inside the reply pump. This catches the rest: conversations that
	// said something worth keeping and then went quiet. Hourly, because the
	// threads it looks for have been idle for six hours already.
	worker.Register(jobs.KindSweepMemories,
		memories.NewExtractionSweeper(memoryExtract.Conversations, queue, log).HandleSweep)

	// Reclaiming space, nothing more: a closed rate-limit window is never read
	// again. Daily rather than hourly because the rows are tiny and keeping a
	// day of them is what lets an operator explain a refusal after the fact.
	worker.Register(jobs.KindSweepQuotas,
		quota.NewService(quota.NewRepository(pool), nil, nil).HandleSweep)

	nudgeSvc := nudges.NewService(nudges.NewRepository(pool), userSvc, checkinSvc, goalSvc)
	worker.Register(jobs.KindSweepNudges, nudges.NewSweeper(nudgeSvc, log).HandleSweep)

	worker.RegisterPeriodic(15*time.Minute, jobs.KindSweepEmbeddings, struct{}{})
	worker.RegisterPeriodic(time.Hour, jobs.KindSweepMemories, struct{}{})
	worker.RegisterPeriodic(time.Hour, jobs.KindSweepNudges, struct{}{})
	worker.RegisterPeriodic(24*time.Hour, jobs.KindSweepQuotas, struct{}{})

	log.Info("worker ready",
		slog.String("ai_provider", registry.DefaultName()),
		slog.String("bucket", cfg.Storage.Bucket),
		slog.Bool("embeddings", documentEmbedder.Enabled()),
	)

	return worker.Run(ctx)
}

// syncStravaHandler adapts the service to the worker's Handler signature.
// The payload type lives in jobs so neither side has to import the other.
func syncStravaHandler(svc *strava.Service) jobs.Handler {
	return func(ctx context.Context, raw json.RawMessage) error {
		var payload jobs.SyncStravaPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
		return svc.HandleSyncJob(ctx, payload.UserID)
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}

	if cfg.Env.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts)).With(slog.String("service", "worker"))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts)).With(slog.String("service", "worker"))
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
