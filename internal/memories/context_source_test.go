package memories_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/memories/memory"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func TestContextSourceFillsMemories(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-fill@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	m, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryInjury,
		Content:  "Left knee is sore on deep squats",
	})
	if err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Memories) != 1 {
		t.Fatalf("want 1 memory in context, got %d", len(into.Memories))
	}
	got := into.Memories[0]
	if got.Ref != coach.MemoryRef(m.ID) {
		t.Errorf("Ref = %q, want %q", got.Ref, coach.MemoryRef(m.ID))
	}
	if got.Label != "profile fact" {
		t.Errorf("Label = %q, want %q", got.Label, "profile fact")
	}
	if !strings.Contains(got.Text, "Left knee is sore on deep squats") {
		t.Errorf("Text missing the stored fact: %q", got.Text)
	}
	if !strings.Contains(got.Text, "[injury]") {
		t.Errorf("Text should carry the category, got %q", got.Text)
	}

	rendered := into.Render()
	if !strings.Contains(rendered, "Known about them:") {
		t.Fatalf("rendered context missing the memories heading:\n%s", rendered)
	}
	wantLine := "- [[" + got.Ref + "]] " + got.Text
	if !strings.Contains(rendered, wantLine) {
		t.Fatalf("memory should render as a cited bullet:\nwant %q\n\n%s", wantLine, rendered)
	}
}

func TestContextSourceStaysEmptyWhenNone(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "source-empty@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(context.Background(), coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Memories) != 0 {
		t.Fatalf("empty store must leave Memories untouched, got %#v", into.Memories)
	}
	if !strings.Contains(into.Render(), "Known about them: none recorded yet") {
		t.Fatalf("empty section must be labelled, not omitted:\n%s", into.Render())
	}
}

func TestContextSourceSurfacesErrors(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-fail@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	pool.Close()

	into := &coach.Context{User: user}
	err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into)
	if err == nil {
		t.Fatal("a failing query must be reported, not swallowed into an empty section")
	}
	if len(into.Memories) != 0 {
		t.Fatalf("a failed collect should leave the section untouched, got %#v", into.Memories)
	}
}

func TestContextSourceName(t *testing.T) {
	t.Parallel()
	if got := memories.NewContextSource(nil).Name(); got != "memories" {
		t.Fatalf("Name() = %q, want %q", got, "memories")
	}
}

func TestContextSourceOmitsPendingAndExcluded(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-filter@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	if _, err := svc.InsertExtractions(ctx, user.ID, uuid.Nil, []memories.Proposal{
		{Candidate: extract.Candidate{Category: memory.CategoryHabit, Content: "Sleeps by 22:00 on weeknights", Confidence: 0.9}},
	}); err != nil {
		t.Fatal(err)
	}
	hidden, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryInjury,
		Content:  "Cannot do overhead pressing due to shoulder instability",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetExcluded(ctx, hidden.ID, user.ID, true); err != nil {
		t.Fatal(err)
	}
	kept, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryHabit,
		Content:  "Trains before work most weekdays",
	})
	if err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{
		User:  user,
		Query: "shoulder overhead press weekday training",
	}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Memories) != 1 {
		t.Fatalf("want only the approved, non-excluded fact, got %d", len(into.Memories))
	}
	if into.Memories[0].Ref != coach.MemoryRef(kept.ID) {
		t.Fatalf("kept the wrong fact: %+v", into.Memories[0])
	}
}

func TestContextSourceHonoursCharBudget(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-budget@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	const body = "This is a durable profile fact written long enough that a handful of them overflow the prompt budget for memories."
	if utf8.RuneCountInString(body) < 80 {
		t.Fatal("fixture is too short to overflow the budget")
	}

	for i := range 16 {
		if _, err := svc.Create(ctx, user.ID, memories.Input{
			Category: memories.CategoryGeneral,
			Content:  fmt.Sprintf("%s (%02d extra)", body, i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Memories) == 0 {
		t.Fatal("budget must keep some facts, not drop the section")
	}
	if len(into.Memories) >= 16 {
		t.Fatalf("budget did not drop anything, got %d facts", len(into.Memories))
	}

	used := 0
	for _, e := range into.Memories {
		used += utf8.RuneCountInString(e.Text)
	}
	if used > memories.ContextCharBudget {
		t.Fatalf("memory section is %d runes, budget is %d", used, memories.ContextCharBudget)
	}

	rendered := into.Render()
	if !strings.Contains(rendered, "Known about them:\n- [[memory:") {
		t.Fatalf("budgeted facts must still render as cited bullets:\n%s", rendered)
	}
}

func TestContextSourcePrefersPinnedWithinBudget(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-pinned-budget@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	const pinned = "Vegetarian, will not eat fish either"
	pin, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryConstraint,
		Content:  pinned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetPinned(ctx, pin.ID, user.ID, true); err != nil {
		t.Fatal(err)
	}

	const filler = "Unrelated background detail written long enough that many of them crowd the pinned fact out of a naive recency window."
	for i := range 16 {
		if _, err := svc.Create(ctx, user.ID, memories.Input{
			Category: memories.CategoryGeneral,
			Content:  fmt.Sprintf("%s (%02d)", filler, i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{
		User:  user,
		Query: "how heavy should my deadlift warm-up sets be",
	}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	found := false
	used := 0
	for _, e := range into.Memories {
		used += utf8.RuneCountInString(e.Text)
		if e.Ref == coach.MemoryRef(pin.ID) {
			found = true
			if !strings.Contains(e.Text, "pinned") {
				t.Errorf("pinned fact should be labelled pinned, got %q", e.Text)
			}
		}
	}
	if !found {
		t.Fatal("a pinned fact must keep its seat inside the budget")
	}
	if used > memories.ContextCharBudget {
		t.Fatalf("memory section is %d runes, budget is %d", used, memories.ContextCharBudget)
	}
}
