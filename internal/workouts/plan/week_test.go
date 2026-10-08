package plan

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// upperLower is the four-day split the feature was asked for: Upper A, Lower
// A, Upper B, Lower B on Monday, Tuesday, Thursday and Friday.
func upperLower() Plan {
	p := planWith(
		day("Monday", exercise("Bench", "barbell")),
		day("Tuesday", exercise("Squat", "barbell")),
		day("Thursday", exercise("Row", "barbell")),
		day("Friday", exercise("Deadlift", "barbell")),
	)
	p.Days[0].Focus, p.Days[1].Focus, p.Days[2].Focus, p.Days[3].Focus = "Upper A", "Lower A", "Upper B", "Lower B"
	return p
}

func focuses(p Plan, slots []Slot) []string {
	out := make([]string, 0, len(slots))
	for _, s := range slots {
		out = append(out, s.Weekday+" "+p.Days[s.DayIndex].Focus)
	}
	return out
}

func TestSpreadWeekdays(t *testing.T) {
	t.Parallel()

	for n := 1; n <= 7; n++ {
		got := SpreadWeekdays(n)
		if len(got) != n {
			t.Fatalf("SpreadWeekdays(%d) = %v", n, got)
		}
		if _, err := NormalizeWeekdays(got); err != nil {
			t.Fatalf("SpreadWeekdays(%d) = %v: %v", n, got, err)
		}
	}
	if got := SpreadWeekdays(0); len(got) != 0 {
		t.Fatalf("SpreadWeekdays(0) = %v, want none", got)
	}
	if got := SpreadWeekdays(8); len(got) != 0 {
		t.Fatalf("SpreadWeekdays(8) = %v, want none", got)
	}
}

func TestNormalizeWeekdays(t *testing.T) {
	t.Parallel()

	got, err := NormalizeWeekdays([]string{"friday", " Monday", "Wednesday"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Monday", "Wednesday", "Friday"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := NormalizeWeekdays([]string{"Monday", "monday"}); err == nil {
		t.Fatal("a repeated weekday should be refused")
	}
	if _, err := NormalizeWeekdays([]string{"Funday"}); err == nil {
		t.Fatal("an unknown weekday should be refused")
	}
}

func TestDefaultWeekIsThePlanAsWritten(t *testing.T) {
	t.Parallel()

	p := upperLower()
	got := focuses(p, DefaultWeek(p, uuid.New(), 0))
	want := []string{"Monday Upper A", "Tuesday Lower A", "Thursday Upper B", "Friday Lower B"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDefaultWeekdaysFallsBackWhenLabelsAreUnusable(t *testing.T) {
	t.Parallel()

	p := planWith(day("Day 1"), day("Day 2"), day("Day 3"))
	if got, want := DefaultWeekdays(p), SpreadWeekdays(3); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	dupes := planWith(day("Monday"), day("Monday"))
	if got, want := DefaultWeekdays(dupes), SpreadWeekdays(2); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRotationFollowsWeekdayOrderNotWrittenOrder(t *testing.T) {
	t.Parallel()

	p := planWith(day("Friday"), day("Monday"), day("Wednesday"))
	if got, want := RotationOrder(p), []int{1, 2, 0}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestThreeDayWeekCarriesTheLeftoverSessionIntoNextWeek(t *testing.T) {
	t.Parallel()

	p, intake := upperLower(), uuid.New()
	week := Fill(p, intake, SpreadWeekdays(3), 0)
	if got, want := focuses(p, week), []string{"Monday Upper A", "Wednesday Lower A", "Friday Upper B"}; !slices.Equal(got, want) {
		t.Fatalf("this week = %v, want %v", got, want)
	}

	cursor, ok := Resume(p, intake, week, []string{"Monday", "Wednesday", "Friday"})
	if !ok {
		t.Fatal("the week had the plan's sessions")
	}
	next := DefaultWeek(p, intake, cursor)
	if got, want := focuses(p, next), []string{"Monday Lower B", "Tuesday Upper A", "Thursday Lower A", "Friday Upper B"}; !slices.Equal(got, want) {
		t.Fatalf("next week = %v, want %v", got, want)
	}
}

func TestSixDaysOfAFourDayPlanWrap(t *testing.T) {
	t.Parallel()

	p := upperLower()
	got := focuses(p, Fill(p, uuid.New(), SpreadWeekdays(6), 0))
	want := []string{"Monday Upper A", "Tuesday Lower A", "Wednesday Upper B", "Thursday Lower B", "Friday Upper A", "Saturday Lower A"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResume(t *testing.T) {
	t.Parallel()

	p, intake := upperLower(), uuid.New()
	week := DefaultWeek(p, intake, 0)

	t.Run("a full week starts over", func(t *testing.T) {
		got, _ := Resume(p, intake, week, []string{"Monday", "Tuesday", "Thursday", "Friday"})
		if mod(got, 4) != 0 {
			t.Fatalf("cursor = %d, want the start of the rotation", got)
		}
	})
	t.Run("a skipped middle session is passed over", func(t *testing.T) {
		got, _ := Resume(p, intake, week, []string{"Monday", "Thursday"})
		if got != 3 {
			t.Fatalf("cursor = %d, want 3 (Lower B)", got)
		}
	})
	t.Run("an empty week repeats where it started", func(t *testing.T) {
		started := Fill(p, intake, SpreadWeekdays(2), 2)
		got, _ := Resume(p, intake, started, nil)
		if got != 2 {
			t.Fatalf("cursor = %d, want 2", got)
		}
	})
	t.Run("a week without the plan says so", func(t *testing.T) {
		if _, ok := Resume(p, uuid.New(), week, nil); ok {
			t.Fatal("another plan's week should not move this plan's rotation")
		}
	})
}

func TestRebuildMidWeek(t *testing.T) {
	t.Parallel()

	p, intake := upperLower(), uuid.New()
	week := DefaultWeek(p, intake, 0)

	t.Run("four to three keeps what was done and carries on", func(t *testing.T) {
		got := Rebuild(Change{
			Today: 2, Existing: week, Done: []string{"Monday"},
			Weekdays: []string{"Monday", "Wednesday", "Saturday"},
			Source:   p, SourceIntake: intake,
		})
		want := []string{"Monday Upper A", "Wednesday Lower A", "Saturday Upper B"}
		if g := focuses(p, got); !slices.Equal(g, want) {
			t.Fatalf("got %v, want %v", g, want)
		}
	})
	t.Run("three to six adds sessions after the last one done", func(t *testing.T) {
		three := Fill(p, intake, SpreadWeekdays(3), 0)
		got := Rebuild(Change{
			Today: 1, Existing: three, Done: []string{"Monday"},
			Weekdays: SpreadWeekdays(6),
			Source:   p, SourceIntake: intake,
		})
		want := []string{"Monday Upper A", "Tuesday Lower A", "Wednesday Upper B", "Thursday Lower B", "Friday Upper A", "Saturday Lower A"}
		if g := focuses(p, got); !slices.Equal(g, want) {
			t.Fatalf("got %v, want %v", g, want)
		}
	})
	t.Run("past days not trained are dropped", func(t *testing.T) {
		got := Rebuild(Change{
			Today: 3, Existing: week,
			Weekdays: []string{"Monday", "Thursday", "Saturday"},
			Source:   p, SourceIntake: intake,
		})
		want := []string{"Thursday Upper A", "Saturday Lower A"}
		if g := focuses(p, got); !slices.Equal(g, want) {
			t.Fatalf("got %v, want %v", g, want)
		}
	})
	t.Run("a pinned day takes another plan's session without using the rotation", func(t *testing.T) {
		mobility := uuid.New()
		got := Rebuild(Change{
			Today: -1, Weekdays: []string{"Monday", "Wednesday", "Friday"},
			Assign: map[string]Slot{"Wednesday": {IntakeID: mobility, DayIndex: 0}},
			Source: p, SourceIntake: intake,
		})
		if len(got) != 3 || got[1].IntakeID != mobility || got[1].Weekday != "Wednesday" {
			t.Fatalf("Wednesday should be the pinned session: %+v", got)
		}
		if p.Days[got[2].DayIndex].Focus != "Lower A" {
			t.Fatalf("Friday = %s, want Lower A", p.Days[got[2].DayIndex].Focus)
		}
	})
	t.Run("a rest week is allowed", func(t *testing.T) {
		if got := Rebuild(Change{Today: -1, Source: p, SourceIntake: intake}); len(got) != 0 {
			t.Fatalf("got %+v, want no slots", got)
		}
	})
}

func TestNextSlot(t *testing.T) {
	t.Parallel()

	p, intake := upperLower(), uuid.New()
	week := Fill(p, intake, []string{"Tuesday", "Thursday", "Saturday"}, 0)
	wednesday := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

	got, ok := NextSlot(week, wednesday, []string{"Tuesday"})
	if !ok || got.Weekday != "Thursday" {
		t.Fatalf("got %+v %v, want Thursday", got, ok)
	}

	saturday := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	if _, ok = NextSlot(week, saturday, []string{"Saturday"}); ok {
		t.Fatal("nothing is left once Saturday is done")
	}

	// A day trained early still counts: Thursday's session done on Wednesday.
	got, ok = NextSlot(week, wednesday, []string{"Tuesday", "Thursday"})
	if !ok || got.Weekday != "Saturday" {
		t.Fatalf("got %+v %v, want Saturday", got, ok)
	}
}

func TestSuggestWeekdays(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		n      int
		today  int
		locked []string
		want   []string
	}{
		{"a week not started gets the spread", 3, -1, nil, []string{"Monday", "Wednesday", "Friday"}},
		{"Monday with nothing done gets the spread", 4, 0, nil, SpreadWeekdays(4)},
		{"midweek spreads over what is left", 3, 2, nil, []string{"Wednesday", "Friday", "Sunday"}},
		{"days already trained count", 3, 2, []string{"Monday"}, []string{"Monday", "Wednesday", "Sunday"}},
		{"more days than are left takes them all", 6, 4, []string{"Monday"}, []string{"Monday", "Friday", "Saturday", "Sunday"}},
		{"one more day is the soonest", 2, 3, []string{"Monday"}, []string{"Monday", "Thursday"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SuggestWeekdays(tc.n, tc.today, tc.locked); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
