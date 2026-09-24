package care

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestCareShapes(t *testing.T) {
	t.Parallel()

	quality := 4
	apitest.AssertGolden(t, "care.golden.json", CareView{
		Water: WaterView{TotalML: 1250, TargetML: 2500, Entries: []WaterEntryView{
			{ID: uuid.MustParse("e1e1e1e1-e1e1-e1e1-e1e1-e1e1e1e1e1e1"), AmountML: 500, LoggedAt: time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)},
		}},
		LastNight: &SleepView{LocalDate: "2026-09-24", DurationMinutes: 452, Quality: &quality, Bedtime: "23:10", WakeTime: "06:45"},
		Habits: []HabitView{{
			ID: uuid.MustParse("f2f2f2f2-f2f2-f2f2-f2f2-f2f2f2f2f2f2"), Name: "Stretch 10 minutes", Domain: "health",
			DaysOfWeek: []int{1, 3, 5}, Streak: 4, Kept: 5, Scheduled: 6, DoneToday: true, ScheduledToday: true,
		}},
		Reminders: []ReminderView{{
			ID: uuid.MustParse("a3a3a3a3-a3a3-a3a3-a3a3-a3a3a3a3a3a3"), Label: "Protein after training",
			TimeOfDay: "19:30", DaysOfWeek: []int{}, Enabled: true, Due: true,
		}},
		CheckedInToday: true,
		WaterWeek:      []CarePoint{{Label: "Mon", Value: 2100}, {Label: "Tue", Value: 1250}},
		SleepWeek:      []CarePoint{{Label: "Mon", Value: 7.1}, {Label: "Tue", Value: 7.5}},
		HabitRate:      83,
	})
}
