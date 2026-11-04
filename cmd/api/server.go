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
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/lifecycle"
	"github.com/NorthAIProject/north-client/internal/shared/metrics"
	"github.com/NorthAIProject/north-client/internal/shared/profiling"
	"github.com/NorthAIProject/north-client/internal/social/phone"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/joho/godotenv"
)

// drainTimeout bounds waiting for detached work at shutdown: replies being
// generated and Telegram turns. Longer than most replies take and shorter than
// the pod's grace period once the 30 second graceful stop has been taken out. A
// turn that outlasts it is cut off, exactly as every turn was before this.
const drainTimeout = 75 * time.Second

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
		metricsReg.WatchPool(func() metrics.PoolStats { return database.PoolStats(pool) })

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

	if cfg.PprofListenAddr != "" {
		defer profiling.Start(cfg.PprofListenAddr, log)()
	}

	background := lifecycle.NewTracker(log)

	handler, runBackground := routes(cfg, background, pool, registry, storage, embedder, posthogClient, metricsReg)

	if runBackground != nil {
		background.Go("background worker", func() {
			if err := runBackground(ctx); err != nil {
				log.Error("background worker stopped", slog.Any("error", err))
			}
		})
	}

	// Serve returns once ctx is cancelled and the requests in flight have had
	// their 30 seconds to finish. Detached work then gets drainTimeout. The pod's
	// terminationGracePeriodSeconds in the infra repo has to cover both.
	log.Info("server starting", slog.String("addr", cfg.Addr()), slog.String("env", string(cfg.Env)))
	serveErr := echox.Serve(ctx, handler, echox.ServerConfig{
		Addr:            cfg.Addr(),
		GracefulTimeout: 30 * time.Second,
		Logger:          log,
	})

	// Before the deferred closes: replies and Telegram turns detached from their
	// requests still need the pool and PostHog to finish.
	log.Info("draining background work", slog.Duration("limit", drainTimeout))
	if !background.Drain(drainTimeout) {
		log.Warn("background work was still running at the drain limit")
	}

	if serveErr != nil {
		return fmt.Errorf("server: %w", serveErr)
	}
	log.Info("server stopped")
	return nil
}

// phoneVerifier picks how phone codes are texted, on the same reasoning as
// mailer: Twilio when configured; in development a stand-in that texts
// nothing and accepts 000000, so the journey can be walked locally; in
// production without Twilio, nothing, which hides phone numbers entirely.
func phoneVerifier(cfg *config.Config) phone.Verifier {
	switch {
	case cfg.Twilio.Enabled():
		return phone.TwilioVerifier{
			AccountSID: cfg.Twilio.AccountSID,
			AuthToken:  cfg.Twilio.AuthToken,
			ServiceSID: cfg.Twilio.VerifyServiceSID,
		}
	case !cfg.Env.IsProduction():
		return phone.DevVerifier{}
	}
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
// Today: the crew live listener, which holds one LISTEN connection for every
// open crew stream, and the Telegram poller, which has to keep asking for
// updates for as long as the process runs. It is returned rather than
// started here because routes() has no lifecycle — no context to cancel, no
// place to report a failure — and a goroutine leaked out of a constructor is
// how a process ends up with two pollers after a test.
type background func(ctx context.Context) error
