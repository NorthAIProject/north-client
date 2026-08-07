// Command mcp-server exposes North's capabilities over the Model Context
// Protocol, so an MCP client — Claude Desktop, or the Hermes bridge that
// carries Telegram and WhatsApp — can use them.
//
// The tools are not defined here. They come from internal/agent, the same
// registry the coach's chat loop reads, so a capability added once appears in
// both. Two definitions would drift, and the drift would show up as the coach
// and Telegram giving different answers to the same question.
//
// # Who the tools run as
//
// One process serves one person, named by NORTH_MCP_USER_EMAIL and resolved to
// an account at startup. There is no per-request authentication because there
// are no per-request credentials to carry: this speaks MCP over stdio, where
// the client spawns the process and the pipe is the session.
//
// That is a property worth stating rather than a shortcut. The user id handed
// to every capability is fixed before the first request is read, so no
// argument a model invents can reach another account's data. Serving several
// people would mean an HTTP transport and real per-request credentials, and
// that is the point at which to add them — not before.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NorthAIProject/north-client/internal/agent"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/users"
)

const serverName = "north"

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

	// stderr, never stdout: stdout is the protocol stream, and a stray log
	// line there is a parse error at the other end rather than a log line.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	email := os.Getenv("NORTH_MCP_USER_EMAIL")
	if email == "" {
		return fmt.Errorf("NORTH_MCP_USER_EMAIL is required: this server acts for exactly one account")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.ByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("no account for %s: %w", email, err)
	}

	mealsRepo := meals.NewRepository(pool)
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))

	registry := agent.Build(agent.Services{
		Exercises:   exercises.NewService(exercises.NewRepository(pool)),
		Calculator:  calculator.NewService(calculator.NewRepository(pool), biometricSvc),
		Goals:       goals.NewService(goals.NewRepository(pool)),
		Ingredients: meals.NewIngredientService(mealsRepo),
		FoodLog:     meals.NewFoodLogService(mealsRepo),
	})

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: "0.1.0",
	}, nil)

	for _, tool := range registry.Tools() {
		addTool(server, registry, user.ID, tool)
	}

	log.Info("north mcp server ready",
		slog.String("user", user.Email),
		slog.Any("tools", registry.Names()))

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

// addTool publishes one capability.
//
// Server.AddTool rather than the generic mcp.AddTool: the argument schema is
// already described by ai.Schema, and going through a Go type just to have the
// SDK infer the schema back would mean two descriptions of every tool's
// arguments, which is the duplication this whole package exists to avoid.
func addTool(server *mcp.Server, registry *agent.Registry, userID uuid.UUID, tool ai.Tool) {
	schema, err := json.Marshal(ai.JSONSchema(tool.Parameters))
	if err != nil {
		// Only reachable if a capability declares a schema that cannot be
		// marshalled, which is a programming error present at startup.
		panic("mcp: cannot marshal the schema for " + tool.Name + ": " + err.Error())
	}

	server.AddTool(&mcp.Tool{
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: json.RawMessage(schema),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// userID is the one resolved at startup. Nothing in the request can
		// change it.
		result := registry.Invoke(ctx, userID, ai.ToolCall{
			ID:        tool.Name,
			Name:      tool.Name,
			Arguments: req.Params.Arguments,
		})

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: result.Content}},
			IsError: result.IsError,
		}, nil
	})
}
