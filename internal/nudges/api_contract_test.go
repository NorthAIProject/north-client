package nudges

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestNudgeShapes(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "nudges.golden.json", NudgeList{Nudges: []NudgeView{{
		ID: uuid.MustParse("77777777-aaaa-aaaa-aaaa-777777777777"), Kind: "checkin_missed", Title: "No check-in for three days",
		Body: "Thirty seconds keeps the thread.", Href: "/app/check-ins", Unread: true, CreatedAt: time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC),
	}}, Unread: 1})
}
