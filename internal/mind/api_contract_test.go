package mind

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestJournalShapes(t *testing.T) {
	t.Parallel()

	mood := 4
	apitest.AssertGolden(t, "journal.golden.json", Journal{
		Entries: []JournalEntryView{{
			ID:      uuid.MustParse("b4b4b4b4-b4b4-b4b4-b4b4-b4b4b4b4b4b4"),
			Content: "Hard week, but the long run felt good.", Mood: &mood, CreatedAt: time.Date(2026, 9, 24, 21, 0, 0, 0, time.UTC),
		}},
		Trend: MoodTrendView{AverageMood: 3.6, AverageEnergy: 3.2, Count: 9},
	})
}
