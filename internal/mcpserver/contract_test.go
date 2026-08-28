package mcpserver_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NorthAIProject/north-client/internal/agent"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/mcpserver"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

var update = flag.Bool("update", false, "rewrite the tool contract golden file")

// toolContract is the part of a tool an outside agent depends on. Deliberately
// not the whole mcp.Tool: fields the SDK adds later should not fail this test,
// only changes to what Khepri actually promises.
type toolContract struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	ReadOnly    bool            `json:"readOnly"`
	Idempotent  bool            `json:"idempotent,omitempty"`
}

// A published tool surface is an interface other people build against — the
// north-connect skill, Hermes on the VPS, whatever a user has wired up. Renaming
// a tool or changing an argument breaks them silently, at their end, with no
// compile error anywhere in this repository.
//
// The package had no tests at all before this, and CI only compiled the binary.
func TestToolContractIsUnchanged(t *testing.T) {
	got := describeTools(t)

	encoded, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')

	golden := filepath.Join("testdata", "tools.golden.json")

	if *update {
		if err = os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(golden, encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", golden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v\n\nRun `go test ./internal/mcpserver/ -update` to create it.", err)
	}

	if string(encoded) != string(want) {
		t.Errorf("the published tool contract changed.\n\n"+
			"If the change is intended, run:\n"+
			"    go test ./internal/mcpserver/ -update\n"+
			"and review the diff — it is what every connected agent will see.\n\n"+
			"got:\n%s\nwant:\n%s", encoded, want)
	}
}

// Every tool must say whether it writes.
//
// The five read-only registry capabilities used to arrive with no annotations
// at all, which made them indistinguishable from calculate_macros — a tool that
// saves the plan it computes. A client honouring read-only mode had to either
// refuse all of them or write when it was told not to.
func TestEveryToolDeclaresWhetherItWrites(t *testing.T) {
	writers := map[string]bool{
		"add_goal_update":  true,
		"create_check_in":  true,
		"create_goal":      true,
		"ask_coach":        true,
		"calculate_macros": true,

		// Editing a training plan. Each inserts a new plan row, and the coach
		// holds every one of them behind an approval card for exactly that
		// reason — see internal/agent/workout_edits.go.
		"swap_workout_exercise":   true,
		"add_workout_exercise":    true,
		"remove_workout_exercise": true,
	}

	for _, tool := range describeTools(t) {
		if tool.ReadOnly == writers[tool.Name] {
			t.Errorf("%s reports readOnly=%t; it %s",
				tool.Name, tool.ReadOnly,
				map[bool]string{true: "writes", false: "only reads"}[writers[tool.Name]])
		}
	}
}

// The registry's schemas are hand-marshalled into MCP rather than inferred by
// the SDK, so nothing else checks that the conversion produced usable JSON.
func TestRegistrySchemasSurviveTheConversion(t *testing.T) {
	for _, tool := range describeTools(t) {
		if len(tool.InputSchema) == 0 {
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("%s has an unparseable input schema: %v", tool.Name, err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("%s declares a %v input schema; MCP arguments are always an object",
				tool.Name, schema["type"])
		}
	}
}

// describeTools registers every tool onto a server and reads back what a client
// would be shown.
func describeTools(t *testing.T) []toolContract {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "north", Version: "test"}, nil)

	// Nil services: registration only declares tools, and no tool is invoked
	// here. A tool that touched a service at registration time would be doing
	// work at the wrong moment, and this would catch that too.
	mcpserver.Register(server, mcpserver.Services{Agent: testRegistry()}, users.User{ID: uuid.New()})

	client := mcp.NewClient(&mcp.Implementation{Name: "contract-test", Version: "test"}, nil)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()

	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	out := make([]toolContract, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		c := toolContract{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: normalise(t, tool.InputSchema),
		}
		if tool.Annotations != nil {
			c.ReadOnly = tool.Annotations.ReadOnlyHint
			c.Idempotent = tool.Annotations.IdempotentHint
		}
		out = append(out, c)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// normalise re-marshals a schema so the golden file does not churn on
// whitespace or key ordering that carries no meaning.
func normalise(t *testing.T, raw any) json.RawMessage {
	t.Helper()
	if raw == nil {
		return nil
	}
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) == "null" {
		return nil
	}
	return out
}

// testRegistry builds the real capability list against services with no
// database behind them.
//
// agent.Build skips any service that is nil, so passing an empty Services would
// publish no tools and the contract would silently cover half the surface. The
// services are constructed with a nil pool instead: declaring a tool never
// touches the database, and this test never invokes one.
func testRegistry() *agent.Registry {
	return agent.Build(agent.Services{
		Exercises:   exercises.NewService(exercises.NewRepository(nil)),
		Calculator:  calculator.NewService(calculator.NewRepository(nil), nil),
		Goals:       goals.NewService(goals.NewRepository(nil)),
		Ingredients: meals.NewIngredientService(meals.NewRepository(nil)),
		FoodLog:     meals.NewFoodLogService(meals.NewRepository(nil)),
		CheckIns:    checkins.NewService(checkins.NewRepository(nil), nil),
		Documents:   documents.NewService(documents.NewRepository(nil), nil, nil),
		Workouts:    workouts.NewService(workouts.Options{Repository: workouts.NewRepository(nil)}),
		Users:       users.NewService(users.NewRepository(nil)),
	})
}
