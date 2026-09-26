package milestones_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/milestones"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestTrackersShape(t *testing.T) {
	t.Parallel()
	six := 6
	apitest.AssertGolden(t, "trackers.golden.json", milestones.ProjectList([]milestones.Tracker{{
		ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Name: "Dentist",
		LastDoneOn: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), IntervalMonths: &six,
	}}, time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)))
}
