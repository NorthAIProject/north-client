package achievements

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestBadgeFeedItemUsesLegalCategory(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	feed := projectFeed(context.Background(), []Item{{
		ID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), UserID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		DisplayName: "Ana", Handle: "ana_runs", Category: "awards", Kind: "badge_earned",
		Title: "First steps", Detail: "Finish a workout", OccurredAt: at, SourceKey: "workouts_1",
		BadgeID: "workouts_1", Badges: []string{"workouts_1", "pr_1"}, Kudos: 2,
	}})
	it := feed.Items[0]
	if it.Category != "goals" {
		t.Fatalf("category = %q, want goals", it.Category)
	}
	if it.BadgeID != "workouts_1" || it.Area != "training" || len(it.Badges) != 2 || it.Badges[1].Area != "lifts" {
		t.Fatalf("item = %+v", it)
	}
	apitest.AssertGolden(t, "feed-badge.golden.json", feed)
}
