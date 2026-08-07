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

	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/shared/database"
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

	// Strava syncs run here rather than in the request that triggered them,
	// so a slow or rate-limited provider never holds a page open.
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	stravaSvc := strava.NewService(strava.Options{
		Repository:   strava.NewRepository(pool),
		Activity:     activity.NewService(activity.NewRepository(pool), biometricSvc),
		Biometrics:   biometricSvc,
		Queue:        queue,
		ClientID:     cfg.StravaClientID,
		ClientSecret: cfg.StravaClientSecret,
		BaseURL:      cfg.BaseURL,
	})

	worker := jobs.NewWorker(queue, log)
	worker.Register(jobs.KindAnalyzeFormVideo, mediaSvc.AnalyzeVideo)
	worker.Register(jobs.KindExtractMemories, memoryExtract.HandleExtractJob)
	worker.Register(jobs.KindSyncStrava, syncStravaHandler(stravaSvc))

	log.Info("worker ready",
		slog.String("ai_provider", registry.DefaultName()),
		slog.String("bucket", cfg.Storage.Bucket),
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
