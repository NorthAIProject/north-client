package coach_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeSource struct {
	name string
	err  error
	fill func(*coach.Context)
}

func (f fakeSource) Name() string { return f.name }

func (f fakeSource) Collect(_ context.Context, _ coach.ContextRequest, into *coach.Context) error {
	if f.err != nil {
		return f.err
	}
	if f.fill != nil {
		f.fill(into)
	}
	return nil
}

func testUser() users.User {
	return users.User{DisplayName: "Fernando", Timezone: "Europe/Lisbon"}
}

func TestNewSectionsRenderWithEmptyStateLabels(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))
	builder := coach.NewContextBuilder(convos)

	ctx, err := builder.Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	rendered := ctx.Render()
	for _, want := range []string{
		"Fitness & nutrition targets: not calculated yet",
		"Today's nutrition: nothing logged yet",
		"Sleep & hydration: nothing logged yet",
		"Habits: none set up yet",
		"Preferences: not set yet",
		"Reflections: none yet",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered context missing %q:\n%s", want, rendered)
		}
	}
}

func TestNewSectionsRenderContentWhenSourcesFillThem(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	builder := coach.NewContextBuilder(convos,
		fakeSource{name: "fitness", fill: func(c *coach.Context) {
			c.FitnessSummary = append(c.FitnessSummary, "Maintenance target: 2136 kcal/day")
		}},
		fakeSource{name: "nutrition", fill: func(c *coach.Context) { c.Nutrition = append(c.Nutrition, "2 Jan: 1800/2136 kcal logged") }},
		fakeSource{name: "preferences", fill: func(c *coach.Context) { c.Preferences = append(c.Preferences, "Units: metric") }},
		fakeSource{name: "reflections", fill: func(c *coach.Context) { c.Reflections = append(c.Reflections, "Feeling good this week") }},
		// Two sources sharing one heading, the arrangement the DailySignals
		// field exists for.
		fakeSource{name: "sleep", fill: func(c *coach.Context) {
			c.DailySignals = append(c.DailySignals, "Sleep: averaging 6.2h over the last 5 nights")
		}},
		fakeSource{name: "hydration", fill: func(c *coach.Context) {
			c.DailySignals = append(c.DailySignals, "Water today: 1.5L of a 2.0L target (75%), across 4 drinks")
		}},
		fakeSource{name: "habits", fill: func(c *coach.Context) {
			c.Habits = append(c.Habits, "Meditate (personal, every day): kept 5 of 6 (83%), 2 day streak")
		}},
	)

	ctx, err := builder.Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	rendered := ctx.Render()
	for _, want := range []string{
		"Maintenance target: 2136 kcal/day",
		"1800/2136 kcal logged",
		"Units: metric",
		"Feeling good this week",
		"averaging 6.2h over the last 5 nights",
		"1.5L of a 2.0L target",
		"2 day streak",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered context missing %q:\n%s", want, rendered)
		}
	}
}

// TestAFailingSourceDoesNotAbortTheOthers is the fail-soft contract every new
// ContextSource in this codebase depends on: a coach that cannot reach one
// domain's data should still answer using everything else it has.
func TestAFailingSourceDoesNotAbortTheOthers(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	builder := coach.NewContextBuilder(convos,
		fakeSource{name: "broken", err: errors.New("database is on fire")},
		fakeSource{name: "fine", fill: func(c *coach.Context) { c.Preferences = append(c.Preferences, "Units: metric") }},
	)

	ctx, err := builder.Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("a failing source should not fail the whole build, got %v", err)
	}

	rendered := ctx.Render()
	if !strings.Contains(rendered, "Units: metric") {
		t.Fatalf("the working source's data should still be present:\n%s", rendered)
	}
}

// TestCheckInsRenderUnderTheirHeading covers the coach half of NOR-10: whatever
// the check-ins source collects has to reach the prompt under a heading the
// model can attribute, and its absence has to stay explicit rather than silent.
func TestCheckInsRenderUnderTheirHeading(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	const summary = "6 Aug — mood 4/5, energy 2/5. Notes: sleep was the limiter"

	filled, err := coach.NewContextBuilder(convos,
		fakeSource{name: "check-ins", fill: func(c *coach.Context) {
			c.CheckIns = append(c.CheckIns, summary)
		}},
	).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	rendered := filled.Render()
	if !strings.Contains(rendered, "Recent check-ins:\n- "+summary) {
		t.Fatalf("check-ins should render as bullets under their heading:\n%s", rendered)
	}

	empty, err := coach.NewContextBuilder(convos).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(empty.Render(), "Recent check-ins: none recorded yet") {
		t.Fatalf("an empty section must say so rather than go missing:\n%s", empty.Render())
	}
}
