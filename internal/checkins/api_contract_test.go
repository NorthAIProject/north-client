package checkins

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestCheckInShapes(t *testing.T) {
	t.Parallel()

	goal := uuid.MustParse("b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2")
	today := CheckInView{
		ID: uuid.MustParse("e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5"), LocalDate: "2026-09-24", Mood: 4, Energy: 3,
		Wins: "Long run done.", Challenges: "Slept badly.", RelatedGoalID: &goal, RelatedGoalTitle: "Run a half marathon",
		UpdatedAt: time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC),
	}
	apitest.AssertGolden(t, "check-ins.golden.json", CheckInList{Today: &today, Recent: []CheckInView{today}, Streak: 6})
}
