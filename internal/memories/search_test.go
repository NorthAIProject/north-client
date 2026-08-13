package memories_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// TestSearchFindsAFactRecencyWouldHaveMissed is the claim the whole retrieval
// change rests on. If it does not hold, ranked search is not earning its keep
// and the document work planned on top of it should not start.
func TestSearchFindsAFactRecencyWouldHaveMissed(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "ranked@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	const buried = "Left shoulder dislocates on wide-grip overhead pressing"

	if _, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryInjury,
		Content:  buried,
	}); err != nil {
		t.Fatal(err)
	}

	// Twenty-five newer, unrelated facts. The context window is twenty, so the
	// shoulder fact is now outside it by recency alone.
	for i := range 25 {
		if _, err := svc.Create(ctx, user.ID, memories.Input{
			Category: memories.CategoryGeneral,
			Content:  fmt.Sprintf("Unrelated background detail number %d about their week", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	byRecency, err := svc.ForContext(ctx, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if containsContent(byRecency, buried) {
		t.Fatal("the fact was still in the recency window; the test no longer proves anything")
	}

	ranked, err := svc.ForContext(ctx, user.ID, "my shoulder hurts when I press overhead, should I still train?")
	if err != nil {
		t.Fatal(err)
	}

	if !containsContent(ranked, buried) {
		t.Fatalf("ranked retrieval missed the one relevant fact; got %d results:\n%s",
			len(ranked), summarise(ranked))
	}

	// Top of the list, not merely present: a fact the coach has to read past
	// twenty others to reach is one it will not weigh properly.
	top := ranked
	if len(top) > 5 {
		top = top[:5]
	}
	if !containsContent(top, buried) {
		t.Errorf("the relevant fact was retrieved but ranked below the top five:\n%s", summarise(ranked))
	}
}

func TestSearchAlwaysKeepsPinnedFacts(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "pinned-search@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	const pinned = "Vegetarian, will not eat fish either"

	m, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryConstraint,
		Content:  pinned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetPinned(ctx, m.ID, user.ID, true); err != nil {
		t.Fatal(err)
	}

	// A query with nothing to do with diet.
	ranked, err := svc.ForContext(ctx, user.ID, "how heavy should my deadlift warm-up sets be")
	if err != nil {
		t.Fatal(err)
	}
	if !containsContent(ranked, pinned) {
		t.Error("a pinned fact was dropped because it did not match the query")
	}
	if len(ranked) > 0 && ranked[0].Snippet != "" {
		t.Errorf("a non-matching pinned fact carried a snippet: %q", ranked[0].Snippet)
	}
}

// Postgres, not Go, is what makes operator characters safe here — see
// internal/search/query.go on why websearch_to_tsquery is the only tsquery
// function that can be handed human input.
func TestSearchSurvivesOperatorCharacters(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "hostile-search@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	if _, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryHabit,
		Content:  "Trains before work most weekdays",
	}); err != nil {
		t.Fatal(err)
	}

	for _, term := range []string{
		`AND OR NOT NEAR( ") ' --`,
		`"unclosed phrase`,
		`); DROP TABLE user_memories; --`,
		`<>&|!:*`,
		`trains -work "before work"`,
	} {
		if _, err := svc.ForContext(ctx, user.ID, term); err != nil {
			t.Errorf("ForContext(%q) failed: %v", term, err)
		}
	}
}

func TestSearchStaysInsideOneAccount(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "search-owner@north.test")
	stranger := seedUser(t, pool, "search-stranger@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	const secret = "Recovering from a herniated disc at L5"

	if _, err := svc.Create(ctx, owner.ID, memories.Input{
		Category: memories.CategoryInjury,
		Content:  secret,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ForContext(ctx, stranger.ID, "herniated disc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("search crossed accounts: %s", summarise(got))
	}
}

func containsContent(list []memories.Retrieved, content string) bool {
	for _, m := range list {
		if m.Content == content {
			return true
		}
	}
	return false
}

func summarise(list []memories.Retrieved) string {
	var b strings.Builder
	for i, m := range list {
		fmt.Fprintf(&b, "  %2d. rank=%.4f pinned=%t %s\n", i+1, m.Rank, m.Pinned, m.Content)
	}
	return b.String()
}
