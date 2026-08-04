// Command worker runs North's background jobs.
//
// A separate binary from the web server, sharing the same code. Video analysis
// takes minutes and holds a provider connection open; running it inside the web
// process would mean a deploy interrupts work in flight, and one slow job would
// compete with request handling for the same connection pool.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/media"
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

	if err := database.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return fmt.Errorf("database migrations: %w", err)
	}
	log.Info("database migrations applied")

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

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
		Model:      cfg.AI.Model,
	})

	worker := jobs.NewWorker(queue, log)
	worker.Register(jobs.KindAnalyzeFormVideo, mediaSvc.AnalyzeVideo)

	log.Info("worker ready",
		slog.String("ai_provider", registry.DefaultName()),
		slog.String("bucket", cfg.Storage.Bucket),
	)

	return worker.Run(ctx)
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
