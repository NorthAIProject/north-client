package agent

import (
	"testing"
	"time"
)

func TestHabitDaysDefaultsToEveryDay(t *testing.T) {
	t.Parallel()

	days, err := habitDays(nil)
	if err != nil {
		t.Fatalf("days: %v", err)
	}
	if len(days) != 7 {
		t.Errorf("%d days, want all seven", len(days))
	}
}

func TestHabitDaysAcceptsNamesAndTheirBeginnings(t *testing.T) {
	t.Parallel()

	days, err := habitDays([]string{"Monday", "wed", "FRI"})
	if err != nil {
		t.Fatalf("days: %v", err)
	}
	want := []time.Weekday{time.Monday, time.Wednesday, time.Friday}
	for i := range want {
		if days[i] != want[i] {
			t.Errorf("day %d = %s, want %s", i, days[i], want[i])
		}
	}
	for _, bad := range []string{"Someday", "t", ""} {
		if _, err := habitDays([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestLifeEditsAreWrites(t *testing.T) {
	t.Parallel()

	for _, c := range []Capability{updateGoal(nil), createHabit(nil, nil), updateHabit(nil, nil), setTargetWeight(nil)} {
		if c.ReadOnly {
			t.Errorf("%s is marked ReadOnly, so it would change data with no approval card", c.Tool.Name)
		}
	}
}
