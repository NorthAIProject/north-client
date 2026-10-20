package dailyposts

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/dailyposts/post"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestDailyPostShapes(t *testing.T) {
	t.Parallel()

	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	today := post.Post{
		ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Date: day(20), Theme: "hydration",
		Title: "Water before meals", Body: "First paragraph.\n\nSecond paragraph.", AIGenerated: true, Kudos: 3, Kudoed: true,
	}
	earlier := post.Post{
		ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), Date: day(19), Theme: "sleep",
		Title: "A steadier bedtime", Body: "One paragraph.", AIGenerated: true,
	}
	apitest.AssertGolden(t, "daily-posts.golden.json", projectFeed(Feed{Today: &today, Earlier: []post.Post{earlier}}))
	apitest.AssertGolden(t, "daily-posts-empty.golden.json", projectFeed(Feed{}))
}
