package toolaudit_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/toolaudit"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*toolaudit.Service, users.User, *pgxpool.Pool) {
	t.Helper()

	pool := testdb.New(t)
	return toolaudit.NewService(toolaudit.NewRepository(pool)), register(t, pool, "fernando@north.test"), pool
}

func register(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()

	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return user
}

func TestAnExecutedCallIsRecordedWithWhatItWrote(t *testing.T) {
	svc, user, _ := newService(t)
	ctx := context.Background()

	err := svc.Record(ctx, toolaudit.Execution{
		UserID:    user.ID,
		Tool:      "create_check_in",
		Arguments: json.RawMessage(`{"mood":4,"energy":3}`),
		Surface:   toolaudit.SurfaceCoach,
		Outcome:   toolaudit.OutcomeExecuted,
		Detail:    "Logged today's check-in.",
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	list, err := svc.List(ctx, user.ID, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("executions = %d, want 1", len(list))
	}

	got := list[0]
	if got.Tool != "create_check_in" {
		t.Errorf("tool = %q", got.Tool)
	}
	if got.Outcome != toolaudit.OutcomeExecuted {
		t.Errorf("outcome = %q, want executed", got.Outcome)
	}
	if got.Surface != toolaudit.SurfaceCoach {
		t.Errorf("surface = %q, want coach", got.Surface)
	}

	// The arguments are the point: "logged a check-in" does not answer what was
	// written. Compared as JSON because jsonb reformats on the way in.
	var args map[string]int
	if err := json.Unmarshal(got.Arguments, &args); err != nil {
		t.Fatalf("stored arguments are not JSON: %v", err)
	}
	if args["mood"] != 4 || args["energy"] != 3 {
		t.Errorf("arguments = %v, want the ones that were written", args)
	}
}

// A refusal is a decision worth being able to point at later.
func TestADeclinedCallIsDistinguishableFromAnExecutedOne(t *testing.T) {
	svc, user, _ := newService(t)
	ctx := context.Background()

	for _, outcome := range []toolaudit.Outcome{toolaudit.OutcomeExecuted, toolaudit.OutcomeDeclined, toolaudit.OutcomeFailed} {
		if err := svc.Record(ctx, toolaudit.Execution{
			UserID:  user.ID,
			Tool:    "create_goal",
			Surface: toolaudit.SurfaceCoach,
			Outcome: outcome,
		}); err != nil {
			t.Fatalf("record %s: %v", outcome, err)
		}
	}

	list, err := svc.List(ctx, user.ID, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	seen := map[toolaudit.Outcome]int{}
	for _, e := range list {
		seen[e.Outcome]++
	}
	for _, want := range []toolaudit.Outcome{toolaudit.OutcomeExecuted, toolaudit.OutcomeDeclined, toolaudit.OutcomeFailed} {
		if seen[want] != 1 {
			t.Errorf("%s recorded %d times, want 1", want, seen[want])
		}
	}
}

// The MCP surface is the one with no confirmation step in front of it, so its
// writes are the ones this table most needs to answer for.
func TestMCPExecutionsAppearAlongsideTheCoachs(t *testing.T) {
	svc, user, _ := newService(t)
	ctx := context.Background()

	if err := svc.Record(ctx, toolaudit.Execution{
		UserID: user.ID, Tool: "create_check_in",
		Surface: toolaudit.SurfaceCoach, Outcome: toolaudit.OutcomeExecuted,
	}); err != nil {
		t.Fatalf("record coach: %v", err)
	}
	if err := svc.Record(ctx, toolaudit.Execution{
		UserID: user.ID, Tool: "add_goal_update",
		Surface: toolaudit.SurfaceMCP, Outcome: toolaudit.OutcomeExecuted,
	}); err != nil {
		t.Fatalf("record mcp: %v", err)
	}

	list, err := svc.List(ctx, user.ID, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("executions = %d, want both surfaces", len(list))
	}

	surfaces := map[toolaudit.Surface]bool{}
	for _, e := range list {
		surfaces[e.Surface] = true
	}
	if !surfaces[toolaudit.SurfaceCoach] || !surfaces[toolaudit.SurfaceMCP] {
		t.Errorf("surfaces = %v, want both coach and mcp", surfaces)
	}
}

func TestExecutionsAreScopedToOneAccount(t *testing.T) {
	svc, user, pool := newService(t)
	ctx := context.Background()
	stranger := register(t, pool, "stranger@north.test")

	if err := svc.Record(ctx, toolaudit.Execution{
		UserID: user.ID, Tool: "create_goal",
		Surface: toolaudit.SurfaceCoach, Outcome: toolaudit.OutcomeExecuted,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	list, err := svc.List(ctx, stranger.ID, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("another account saw %d executions", len(list))
	}
}

func TestExecutionsComeBackNewestFirst(t *testing.T) {
	svc, user, _ := newService(t)
	ctx := context.Background()

	for _, tool := range []string{"first", "second", "third"} {
		if err := svc.Record(ctx, toolaudit.Execution{
			UserID: user.ID, Tool: tool,
			Surface: toolaudit.SurfaceCoach, Outcome: toolaudit.OutcomeExecuted,
		}); err != nil {
			t.Fatalf("record %s: %v", tool, err)
		}
	}

	list, err := svc.List(ctx, user.ID, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("executions = %d, want 3", len(list))
	}
	if list[0].Tool != "third" {
		t.Errorf("first row = %q, want the most recent", list[0].Tool)
	}
}
