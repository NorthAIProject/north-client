package firstweek_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/firstweek"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestFirstWeekShape(t *testing.T) {
	t.Parallel()
	goal := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	start := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	p := firstweek.Programme{
		UserID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), GoalID: &goal,
		Template: firstweek.TemplateSleep, Timezone: "Europe/Lisbon", StartedOn: start, Status: firstweek.StatusActive,
	}
	today := firstweek.Today{Programme: p, Day: 3, Done: true}
	for d := 1; d <= firstweek.Days; d++ {
		s := firstweek.DayState{Day: d, Date: p.DateOf(d)}
		switch d {
		case 1:
			s.Done, s.Via = true, firstweek.ViaLog
		case 3:
			s.Done, s.Via = true, firstweek.ViaTap
		}
		today.Days = append(today.Days, s)
	}
	apitest.AssertGolden(t, "first_week_active.golden.json", firstweek.Project(context.Background(), today))
	apitest.AssertGolden(t, "first_week_none.golden.json", firstweek.None())
}
