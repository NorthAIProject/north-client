package weekly

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/weekly/week"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

func TestWeeklyShapes(t *testing.T) {
	t.Parallel()

	reviewed := time.Date(2026, 10, 4, 19, 30, 0, 0, time.UTC)
	last := week.Focus{WeekStart: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), Priorities: []string{"Sleep by 23:00"}, Volume: plan.VolumeHold, ReviewedAt: reviewed.AddDate(0, 0, -7)}
	next := week.Focus{WeekStart: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), Priorities: []string{"Three runs", "Finish the deck"}, Volume: plan.VolumeDeload, ReviewedAt: reviewed}

	apitest.AssertGolden(t, "weekly-review.golden.json", projectReview(week.Review{
		Reviewing: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
		Planning:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Report:    &week.Report{ID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), Title: "Week of 28 Sep 2026", Body: "## What moved\n\nFour check-ins.", Ready: true},
		Last:      &last,
		Goals: []week.Goal{
			{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Title: "Run a half marathon", Category: "fitness", Priority: 1},
			{ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), Title: "Ship the portfolio", Category: "work"},
		},
	}))
	apitest.AssertGolden(t, "weekly-focus.golden.json", projectFocus(next))
}
