package achievements

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestFeedShapes(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 7, 30, 0, 0, time.UTC)
	apitest.AssertGolden(t, "feed.golden.json", projectFeed([]Item{{
		ID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), UserID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		DisplayName: "Ana", Handle: "ana_runs", Category: "training", Kind: "workout_completed",
		Title: "Finished Running", Detail: "32 min", OccurredAt: at, Kudos: 3, Kudoed: true,
	}}))
	apitest.AssertGolden(t, "sharing.golden.json", SharingView{Training: true, Streaks: false, Goals: true})
}
