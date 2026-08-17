package coach_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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
		"Decisions: none recorded yet",
		"Latest weekly review: none yet",
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

func TestMemoriesRenderUnderTheirHeading(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	const ref = "memory:6f2c81a4-1111-2222-3333-444444444444"
	const text = "[injury, pinned] Left knee is sore on deep squats"

	filled, err := coach.NewContextBuilder(convos,
		fakeSource{name: "memories", fill: func(c *coach.Context) {
			c.Memories = append(c.Memories, coach.Evidence{
				Ref:   ref,
				Text:  text,
				Label: "profile fact",
			})
		}},
	).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	rendered := filled.Render()
	want := "Known about them:\n- [[" + ref + "]] " + text
	if !strings.Contains(rendered, want) {
		t.Fatalf("memories should render as cited bullets under their heading:\n%s", rendered)
	}

	empty, err := coach.NewContextBuilder(convos).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(empty.Render(), "Known about them: none recorded yet") {
		t.Fatalf("an empty memory section must say so rather than go missing:\n%s", empty.Render())
	}
}

func TestDecisionsRenderUnderTheirHeading(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	const summary = "15 Aug — Quit the evening client. Options: keep / quit. Why: energy"

	filled, err := coach.NewContextBuilder(convos,
		fakeSource{name: "decisions", fill: func(c *coach.Context) {
			c.Decisions = append(c.Decisions, summary)
		}},
	).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	rendered := filled.Render()
	if !strings.Contains(rendered, "Decisions:\n- "+summary) {
		t.Fatalf("decisions should render as bullets under their heading:\n%s", rendered)
	}

	empty, err := coach.NewContextBuilder(convos).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(empty.Render(), "Decisions: none recorded yet") {
		t.Fatalf("an empty section must say so rather than go missing:\n%s", empty.Render())
	}
}

// A long thread's earlier turns reach the model as a summary rather than being
// dropped. This is the behaviour NOR-26 exists to add.
func TestBuildPutsTheConversationSummaryInFrontOfTheModel(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))
	userSvc := users.NewService(users.NewRepository(pool))
	ctx := context.Background()

	u, err := userSvc.Register(ctx, users.Registration{
		Email:        "ctx-summary@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}

	c, err := convos.Start(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = convos.AppendUserMessage(ctx, c.ID, "the newest turn", nil); err != nil {
		t.Fatal(err)
	}
	if err = convos.SetContextSummary(ctx, c.ID, "they decided to train five days a week", time.Now()); err != nil {
		t.Fatal(err)
	}

	built, err := coach.NewContextBuilder(convos).Build(ctx, coach.ContextRequest{
		User:           u,
		ConversationID: c.ID,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if built.ConversationSummary != "they decided to train five days a week" {
		t.Fatalf("summary not loaded: %q", built.ConversationSummary)
	}

	rendered := built.Render()
	if !strings.Contains(rendered, "Earlier in this conversation:") {
		t.Fatalf("rendered context has no earlier-conversation block:\n%s", rendered)
	}
	if !strings.Contains(rendered, "they decided to train five days a week") {
		t.Fatalf("rendered context does not carry the summary:\n%s", rendered)
	}
}

// A thread with no summary renders no empty heading.
func TestBuildOmitsTheSummaryBlockWhenThereIsNone(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	built, err := coach.NewContextBuilder(convos).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(built.Render(), "Earlier in this conversation") {
		t.Fatal("rendered an earlier-conversation block with nothing in it")
	}
}

// slowSource blocks until its context is done, or until it has waited longer
// than any deadline the builder should allow.
type slowSource struct {
	name    string
	started chan struct{}
	once    sync.Once
}

func (s *slowSource) Name() string { return s.name }

func (s *slowSource) Collect(ctx context.Context, _ coach.ContextRequest, _ *coach.Context) error {
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Minute):
		return nil
	}
}

// The point of NOR-58: one hanging source must not hold the reply. Before this,
// eighteen sources ran in series and a stuck one blocked everything behind it.
func TestAHangingSourceDoesNotHoldTheReply(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	slow := &slowSource{name: "calendar", started: make(chan struct{})}
	builder := coach.NewContextBuilder(convos,
		slow,
		fakeSource{name: "fine", fill: func(c *coach.Context) { c.Preferences = append(c.Preferences, "Units: metric") }},
	)

	start := time.Now()
	built, err := builder.Build(context.Background(), coach.ContextRequest{User: testUser()})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("a hanging source should not fail the build: %v", err)
	}

	select {
	case <-slow.started:
	default:
		t.Fatal("the slow source never ran")
	}

	// Its own deadline, not a minute. Generous upper bound so a loaded CI box
	// does not make this flaky — the failure being caught is 60s, not 5s.
	if elapsed > 30*time.Second {
		t.Fatalf("build waited %s for a hanging source", elapsed)
	}

	// And the healthy source still contributed.
	if !strings.Contains(built.Render(), "Units: metric") {
		t.Fatal("a hanging source cost the reply a healthy source's data")
	}
}

// Sources run concurrently, so the prompt must still be assembled in
// registration order. A prompt that reordered itself by whichever query
// finished first would make one reply impossible to compare against the next.
func TestContextIsAssembledInRegistrationOrderNotCompletionOrder(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	// The first-registered source is deliberately the slowest to return.
	builder := coach.NewContextBuilder(convos,
		fakeSource{name: "sleep", fill: func(c *coach.Context) {
			time.Sleep(80 * time.Millisecond)
			c.DailySignals = append(c.DailySignals, "Sleep: 6.2h")
		}},
		fakeSource{name: "hydration", fill: func(c *coach.Context) {
			c.DailySignals = append(c.DailySignals, "Water: 1.5L")
		}},
	)

	for range 5 {
		built, err := builder.Build(context.Background(), coach.ContextRequest{User: testUser()})
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if len(built.DailySignals) != 2 {
			t.Fatalf("DailySignals = %v, want both entries", built.DailySignals)
		}
		if built.DailySignals[0] != "Sleep: 6.2h" || built.DailySignals[1] != "Water: 1.5L" {
			t.Fatalf("order follows completion, not registration: %v", built.DailySignals)
		}
	}
}

// Two sources writing the same field concurrently is the race this design
// avoids by giving each its own Context. Run under -race, this is the guard.
func TestSourcesSharingAFieldDoNotRace(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	sources := make([]coach.ContextSource, 0, 12)
	for i := range 12 {
		sources = append(sources, fakeSource{
			name: fmt.Sprintf("writer-%d", i),
			fill: func(c *coach.Context) {
				c.DailySignals = append(c.DailySignals, "signal")
				c.FitnessSummary = append(c.FitnessSummary, "fitness")
			},
		})
	}

	built, err := coach.NewContextBuilder(convos, sources...).
		Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(built.DailySignals) != 12 || len(built.FitnessSummary) != 12 {
		t.Fatalf("lost writes: DailySignals=%d FitnessSummary=%d, want 12 and 12",
			len(built.DailySignals), len(built.FitnessSummary))
	}
}
