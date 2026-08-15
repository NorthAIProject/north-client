package decisions_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/decisions"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func TestContextSourceFillsDecisions(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	if _, err := svc.Create(ctx, user.ID, decisions.Input{
		Title:     "Quit the evening client",
		Options:   "keep / quit",
		Rationale: "energy",
	}); err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	if err := decisions.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Decisions) != 1 {
		t.Fatalf("want 1 decision in context, got %d", len(into.Decisions))
	}
	for _, want := range []string{
		"Quit the evening client",
		"Options: keep / quit",
		"Why: energy",
	} {
		if !strings.Contains(into.Decisions[0], want) {
			t.Errorf("context entry missing %q:\n%s", want, into.Decisions[0])
		}
	}

	rendered := into.Render()
	if !strings.Contains(rendered, "Decisions:") {
		t.Fatalf("rendered context missing the decisions heading:\n%s", rendered)
	}
	if !strings.Contains(rendered, "- "+into.Decisions[0]) {
		t.Fatalf("decision should render as a bullet under its heading:\n%s", rendered)
	}
}

func TestContextSourceStaysEmptyWhenNone(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "empty@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	into := &coach.Context{User: user}
	if err := decisions.NewContextSource(svc).Collect(context.Background(), coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Decisions) != 0 {
		t.Fatalf("want no decisions, got %v", into.Decisions)
	}
}

func TestContextSourceSurfacesErrors(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "sourcefail@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	pool.Close()

	into := &coach.Context{User: user}
	err := decisions.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into)
	if err == nil {
		t.Fatal("a failing query must be reported, not swallowed into an empty section")
	}
	if len(into.Decisions) != 0 {
		t.Fatalf("a failed collect should leave the section untouched, got %v", into.Decisions)
	}
}

func TestContextSourceName(t *testing.T) {
	t.Parallel()
	if got := decisions.NewContextSource(nil).Name(); got != "decisions" {
		t.Fatalf("Name() = %q, want %q — it labels the source in failure logs", got, "decisions")
	}
}

func TestContextSourcePrefersKeywordMatch(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "keyword@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	if _, err := svc.Create(ctx, user.ID, decisions.Input{
		Title: "Quit the evening client",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, user.ID, decisions.Input{Title: "Bought a new bike"}); err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	err := decisions.NewContextSource(svc).Collect(ctx, coach.ContextRequest{
		User:  user,
		Query: "should I quit that client",
	}, into)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Decisions) == 0 {
		t.Fatal("want the matching decision in context")
	}
	if !strings.Contains(into.Decisions[0], "Quit the evening client") {
		t.Fatalf("keyword match should come first:\n%s", into.Decisions[0])
	}
}
