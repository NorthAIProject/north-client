package inbox

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/inbox/item"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestInboxShapes(t *testing.T) {
	t.Parallel()

	goal := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	at := time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC)
	items := []Item{
		{
			ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Text: "Ran 12 km, the knee felt fine", Source: item.SourceApp, CreatedAt: at,
			Suggestion: &Suggestion{Destination: item.DestinationGoalNote, GoalID: &goal, GoalTitle: "Run a half marathon", Why: "It is progress on your half marathon."},
		},
		{
			ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Text: "https://example.com/zone-2 — read later", Source: item.SourceShare, CreatedAt: at.Add(-time.Hour),
		},
	}
	apitest.AssertGolden(t, "inbox.golden.json", projectInbox(items, 2))
	apitest.AssertGolden(t, "inbox-item.golden.json", projectItem(items[1]))
}
