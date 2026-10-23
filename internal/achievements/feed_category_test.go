package achievements

import (
	"context"
	"testing"
)

// Whatever the row, the wire category is one of the shipped app's closed
// values: a stored "awards" never goes out as itself.
func TestProjectFeedNeverSendsAwardsCategory(t *testing.T) {
	t.Parallel()
	got := projectFeed(context.Background(), []Item{{Category: "awards", Kind: "badge_earned", SourceKey: "gone"}})
	if len(got.Items) != 1 || got.Items[0].Category != "goals" {
		t.Fatalf("category = %+v, want goals", got.Items)
	}
}
