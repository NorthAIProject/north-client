package news

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestNewsShapes(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "news.golden.json", NewsTicker{Enabled: true, Items: []NewsItemView{{
		Title: "Short naps and recovery", URL: "https://example.com/naps", Source: "BBC Health",
		PublishedAt: time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC),
	}}})
}
