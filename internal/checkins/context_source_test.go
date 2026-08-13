package checkins_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// TestContextSourceFillsCheckIns is the end-to-end claim NOR-10 makes: a
// check-in the user wrote reaches the block the model actually reads.
func TestContextSourceFillsCheckIns(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source@north.test", "Europe/Lisbon")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	if _, err := svc.UpsertToday(ctx, user, checkins.Input{
		Mood:       4,
		Energy:     2,
		Wins:       "finished the long run",
		Challenges: "calves tight",
		Notes:      "sleep was the limiter this week",
	}); err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	if err := checkins.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(into.CheckIns) != 1 {
		t.Fatalf("want 1 check-in in context, got %d", len(into.CheckIns))
	}
	for _, want := range []string{
		"mood 4/5, energy 2/5",
		"Wins: finished the long run",
		"Challenges: calves tight",
		"Notes: sleep was the limiter this week",
	} {
		if !strings.Contains(into.CheckIns[0], want) {
			t.Errorf("context entry missing %q:\n%s", want, into.CheckIns[0])
		}
	}

	rendered := into.Render()
	if !strings.Contains(rendered, "Recent check-ins:") {
		t.Fatalf("rendered context missing the check-ins heading:\n%s", rendered)
	}
	if !strings.Contains(rendered, "- "+into.CheckIns[0]) {
		t.Fatalf("check-in should render as a bullet under its heading:\n%s", rendered)
	}
}

// TestContextSourceSurfacesErrors holds up this source's half of the fail-soft
// contract. Deciding a failure is survivable is the builder's job
// (TestAFailingSourceDoesNotAbortTheOthers in internal/coach); the source must
// report the failure rather than quietly hand back an empty section, which
// would tell the coach the user has not been reflecting.
func TestContextSourceSurfacesErrors(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "sourcefail@north.test", "UTC")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	pool.Close() // the database is now unreachable

	into := &coach.Context{User: user}
	err := checkins.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into)
	if err == nil {
		t.Fatal("a failing query must be reported, not swallowed into an empty section")
	}
	if len(into.CheckIns) != 0 {
		t.Fatalf("a failed collect should leave the section untouched, got %v", into.CheckIns)
	}
}

func TestContextSourceLabelsAttachedGoal(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "attachedgoal@north.test", "UTC")
	goalSvc := goals.NewService(goals.NewRepository(pool))
	svc := checkins.NewService(checkins.NewRepository(pool), goalSvc)

	g, err := goalSvc.Create(ctx, user.ID, goals.Input{
		Title:    "Run a marathon",
		Category: goals.CategoryFitness,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.UpsertToday(ctx, user, checkins.Input{
		Mood: 3, Energy: 3, RelatedGoalID: &g.ID,
	}); err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	if err := checkins.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.CheckIns) != 1 {
		t.Fatalf("want 1 check-in, got %d", len(into.CheckIns))
	}
	if !strings.Contains(into.CheckIns[0], "(re: Run a marathon)") {
		t.Fatalf("attached check-in should name the goal:\n%s", into.CheckIns[0])
	}
}

func TestContextSourceLeavesDetachedCheckInUnlabeled(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "detachedgoal@north.test", "UTC")
	goalSvc := goals.NewService(goals.NewRepository(pool))
	svc := checkins.NewService(checkins.NewRepository(pool), goalSvc)

	if _, err := goalSvc.Create(ctx, user.ID, goals.Input{
		Title:    "Run a marathon",
		Category: goals.CategoryFitness,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpsertToday(ctx, user, checkins.Input{Mood: 3, Energy: 3}); err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	if err := checkins.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.CheckIns) != 1 {
		t.Fatalf("want 1 check-in, got %d", len(into.CheckIns))
	}
	if strings.Contains(into.CheckIns[0], "(re:") {
		t.Fatalf("detached check-in should not name a goal:\n%s", into.CheckIns[0])
	}
}

func TestContextSourceName(t *testing.T) {
	t.Parallel()
	if got := checkins.NewContextSource(nil).Name(); got != "check-ins" {
		t.Fatalf("Name() = %q, want %q — it labels the source in failure logs", got, "check-ins")
	}
}
