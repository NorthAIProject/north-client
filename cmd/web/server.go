package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/shared/metrics"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/joho/godotenv"
)

// run owns the process lifecycle. Keeping it separate from main means every
// exit path runs deferred cleanup, which os.Exit inside main would skip.
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

	if cfg.AutoMigrate {
		if migrateErr := database.Migrate(ctx, cfg.DatabaseURL); migrateErr != nil {
			return fmt.Errorf("database migrations: %w", migrateErr)
		}
		log.Info("database migrations applied")
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("connected to database")

	spendMeter := spend.NewMeter(spend.NewRepository(pool).WithLogger(log))

	aiOpts := cfg.AI.ProviderOptions(cfg.Env)
	aiOpts.Meter = spendMeter

	registry, err := providers.Build(ctx, aiOpts)
	if err != nil {
		return err
	}
	cfg.AI.LogReady(log, registry)
	cfg.LogSecurityPosture(log)

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

	embedder, err := providers.Embedder(registry, providers.EmbedderOptions{
		Provider:   cfg.Embedding.Provider,
		Model:      cfg.Embedding.Model,
		Dimensions: cfg.Embedding.Dimensions,
		Meter:      spendMeter,
	})
	if err != nil {
		return err
	}

	posthogClient, err := analytics.NewClient(cfg.PostHog, cfg.Env.IsProduction())
	if err != nil {
		return err
	}
	defer func() { _ = posthogClient.Close() }()

	var metricsReg *metrics.Registry
	if cfg.MetricsListenAddr != "" {
		metricsReg = metrics.New()

		metricsSrv := &http.Server{
			Addr:              cfg.MetricsListenAddr,
			Handler:           metricsReg.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			log.Info("metrics listening", slog.String("addr", metricsSrv.Addr))
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("metrics listener stopped", slog.Any("error", err))
			}
		}()
		defer func() { _ = metricsSrv.Close() }()
	}

	handler, runBackground := routes(cfg, pool, registry, storage, embedder, posthogClient, metricsReg)

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,

		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", slog.String("addr", srv.Addr), slog.String("env", string(cfg.Env)))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("server stopped")
	return nil
}

// mailer picks how transactional email leaves the process.
//
// LogMailer is the right answer in development: the reset link lands in the
// terminal the developer is already watching, and no local SMTP is needed to
// exercise the journey. It is the wrong answer in production, which is why
// auth refuses to run the reset routes on it there rather than writing account
// recovery links into a shared log.
func mailer(cfg *config.Config) auth.Mailer {
	if !cfg.SMTP.Enabled() {
		return auth.LogMailer{}
	}
	return auth.SMTPMailer{
		Host:     cfg.SMTP.Host,
		Port:     cfg.SMTP.Port,
		Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password,
		From:     cfg.SMTP.From,
		FromName: cfg.SMTP.FromName,
	}
}

// background is work that outlives any one request.
//
// Today there is exactly one: the Telegram poller, which has to keep asking
// for updates for as long as the process runs. It is returned rather than
// started here because routes() has no lifecycle — no context to cancel, no
// place to report a failure — and a goroutine leaked out of a constructor is
// how a process ends up with two pollers after a test.
type background func(ctx context.Context) error
