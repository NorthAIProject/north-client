// Command mcp-server exposes North's coaching capabilities over the Model
// Context Protocol.
//
// A separate binary from the web server, sharing the same services. It exists
// so an agent that already has the user's attention elsewhere — Hermes on the
// VPS, an editor, a chat client — can log a check-in or ask the coach without
// anybody opening a browser.
//
// # Bind it to the tailnet
//
// Authentication is a single static bearer token mapping to a single account.
// That is enough for one person's private tooling and nowhere near enough for
// the public internet. See internal/mcpserver.NewHandler.
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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/agent"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/mcpserver"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// version is what the MCP handshake advertises.
//
// It was never set, so every client saw the handler's "0.1.0" fallback no
// matter what had shipped — which makes the field worse than absent, because a
// client reading it is being told something false. Bumped here because the
// published contract changed: every tool now declares whether it writes, and
// results carry structured content as well as text. The contract this names is
// internal/mcpserver/testdata/tools.golden.json.
const version = "0.2.0"

const shutdownTimeout = 10 * time.Second

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

	token := strings.TrimSpace(os.Getenv("MCP_API_TOKEN"))
	if token == "" {
		return errors.New("MCP_API_TOKEN is required: the MCP server will not start without a bearer token")
	}

	rawUserID := strings.TrimSpace(os.Getenv("MCP_USER_ID"))
	if rawUserID == "" {
		return errors.New("MCP_USER_ID is required: every tool call acts as one account")
	}
	userID, err := uuid.Parse(rawUserID)
	if err != nil {
		return fmt.Errorf("MCP_USER_ID is not a uuid: %w", err)
	}

	addr := cfg.MCPListenAddr

	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	registry, err := providers.Build(ctx, cfg.AI.ProviderOptions())
	if err != nil {
		return err
	}

	embedder, err := providers.Embedder(registry, providers.EmbedderOptions{
		Provider:   cfg.Embedding.Provider,
		Model:      cfg.Embedding.Model,
		Dimensions: cfg.Embedding.Dimensions,
	})
	if err != nil {
		return err
	}

	handler := mcpserver.NewHandler(mcpserver.Config{
		Services: buildServices(cfg, pool, registry, embedder),
		Token:    token,
		UserID:   userID,
		Log:      log,

		// Empty unless MCP_ALLOWED_ORIGINS says otherwise, which rejects every
		// browser. Real MCP clients send no Origin at all; a request that does
		// is a web page, and a web page is not the intended caller.
		AllowedOrigins:    cfg.MCPAllowedOrigins,
		RequestsPerMinute: cfg.MCPRequestsPerMinute,

		Version: version,
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("mcp server listening",
			slog.String("addr", addr),
			slog.String("user", userID.String()),
			slog.String("ai_provider", registry.DefaultName()),
		)
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errs <- serveErr
		}
	}()

	select {
	case serveErr := <-errs:
		return serveErr
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("mcp server stopped")
	return nil
}

// buildServices constructs what the tools call into.
//
// The coach is given the same context sources cmd/web gives it, because an
// answer from the MCP tool has to be as grounded as one from the web UI —
// a coach that forgot the user's goals when asked through Hermes would be
// worse than no tool at all.
//
// This list is duplicated from cmd/web's routes(). Adding a context source in
// one place and not the other is the drift to watch for; the two are worth
// unifying into a shared composition root once a third binary needs them.
func buildServices(cfg *config.Config, pool *pgxpool.Pool, registry *ai.Registry, embedder ai.Embedder) mcpserver.Services {
	userSvc := users.NewService(users.NewRepository(pool))
	conversationSvc := conversations.NewService(conversations.NewRepository(pool))

	goalSvc := goals.NewService(goals.NewRepository(pool))
	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	memorySvc := memories.NewService(memories.NewRepository(pool))

	// Retrieval only. This process has no object storage and no queue, so it
	// can read a person's indexed documents but cannot add to them or index
	// them — which is the right shape for a surface an agent holds a token to.
	documentSvc := documents.NewService(documents.NewRepository(pool), nil, nil)

	if embedder != nil {
		documentSvc = documentSvc.WithEmbeddings(embedder, slog.Default())
	}

	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	calculatorSvc := calculator.NewService(calculator.NewRepository(pool), biometricSvc)
	activitySvc := activity.NewService(activity.NewRepository(pool), biometricSvc)

	mealsRepo := meals.NewRepository(pool)
	mealDietSvc := meals.NewDietPreferenceService(mealsRepo)
	foodLogSvc := meals.NewFoodLogService(mealsRepo)
	mealProgressSvc := meals.NewTrackMealProgressService(foodLogSvc, calculatorSvc)

	preferencesSvc := preferences.NewService(preferences.NewRepository(pool))
	mindSvc := mind.NewService(mind.NewRepository(pool), checkinSvc)

	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))
	sleepSvc := sleep.NewService(sleep.NewRepository(pool))
	habitSvc := habits.NewService(habits.NewRepository(pool))

	coachSvc := coach.NewService(coach.Options{
		Registry:      registry,
		Conversations: conversationSvc,
		ContextBuilder: coach.NewContextBuilder(conversationSvc,
			goals.NewContextSource(goalSvc),
			checkins.NewContextSource(checkinSvc),
			memories.NewContextSource(memorySvc),
			documents.NewContextSource(documentSvc),
			calculator.NewContextSource(calculatorSvc),
			activity.NewContextSource(activitySvc),
			meals.NewContextSource(mealProgressSvc, mealDietSvc),
			preferences.NewContextSource(preferencesSvc),
			mind.NewContextSource(mindSvc),
			hydration.NewContextSource(hydrationSvc),
			sleep.NewContextSource(sleepSvc),
			habits.NewContextSource(habitSvc),
		),
		PromptBuilder: coach.NewPromptBuilder(),
		Chains:        cfg.AI.ChainSet(),
		Model:         cfg.AI.Model,
		FastModel:     cfg.AI.FastModel,
	})

	agentTools := agent.Build(agent.Services{
		Exercises:   exercises.NewService(exercises.NewRepository(pool)),
		Calculator:  calculatorSvc,
		Goals:       goalSvc,
		Ingredients: meals.NewIngredientService(mealsRepo),
		FoodLog:     foodLogSvc,
	})

	return mcpserver.Services{
		Agent:     agentTools,
		Users:     userSvc,
		Goals:     goalSvc,
		CheckIns:  checkinSvc,
		Memories:  memorySvc,
		Documents: documentSvc,
		Activity:  activitySvc,
		Coach:     coachSvc,
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}

	if cfg.Env.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts)).With(slog.String("service", "mcp-server"))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts)).With(slog.String("service", "mcp-server"))
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
