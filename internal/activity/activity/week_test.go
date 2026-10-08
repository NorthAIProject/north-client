package activity

import (
	"reflect"
	"testing"
	"time"
)

// Which plan day a session finishes decides whether the Training list says
// Completed or Next, so these pin the cases a person would notice: the day
// they trained, the day they trained early for, and the sessions that must
// not count.

// 2026-09-28 is a Monday.
var monday = time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

func on(day int, hour int) time.Time {
	return monday.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour)
}

func lifting(start time.Time, weekday string) Session {
	s := completed("strength_training", start, time.Hour, 300)
	s.PlanWeekday = weekday
	return s
}

func TestWeekStartIsMonday(t *testing.T) {
	t.Parallel()

	for day := 0; day < 7; day++ {
		if got := WeekStart(on(day, 23), time.UTC); !got.Equal(monday) {
			t.Fatalf("day %d: week start = %s, want %s", day, got, monday)
		}
	}
	if got := WeekStart(on(7, 1), time.UTC); !got.Equal(monday.AddDate(0, 0, 7)) {
		t.Fatalf("next Monday belongs to the next week, got %s", got)
	}
}

func TestCompletedWeekdaysCountsTheNamedPlanDay(t *testing.T) {
	t.Parallel()

	// Wednesday's workout done on Tuesday finishes Wednesday, not Tuesday.
	sessions := []Session{lifting(on(1, 18), "Wednesday")}
	got := CompletedWeekdays(sessions, time.UTC, on(2, 9))
	if want := []string{"Wednesday"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed = %v, want %v", got, want)
	}
}

func TestCompletedWeekdaysFallsBackToTheStartDayForStrength(t *testing.T) {
	t.Parallel()

	sessions := []Session{lifting(on(0, 7), "")}
	got := CompletedWeekdays(sessions, time.UTC, on(3, 9))
	if want := []string{"Monday"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed = %v, want %v", got, want)
	}
}

func TestCompletedWeekdaysIgnoresRunsCancelledAndOpenSessions(t *testing.T) {
	t.Parallel()

	run := completed("running", on(0, 7), time.Hour, 500)
	cancelled := lifting(on(1, 7), "Tuesday")
	cancelled.Status = StatusCancelled
	open := lifting(on(2, 7), "Wednesday")
	open.Status = StatusActive
	open.EndedAt = nil

	if got := CompletedWeekdays([]Session{run, cancelled, open}, time.UTC, on(2, 9)); len(got) != 0 {
		t.Fatalf("completed = %v, want none", got)
	}
}

func TestCompletedWeekdaysResetsOnMonday(t *testing.T) {
	t.Parallel()

	// Last week's Saturday is not this week's Saturday.
	sessions := []Session{lifting(on(5, 9), "Saturday")}
	if got := CompletedWeekdays(sessions, time.UTC, on(8, 9)); len(got) != 0 {
		t.Fatalf("completed = %v, want none after the week rolled", got)
	}
}

func TestCompletedWeekdaysUsesTheUsersDay(t *testing.T) {
	t.Parallel()

	lisbon, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Skip("no tzdata")
	}
	// 23:30 UTC on Sunday is 00:30 Monday in Lisbon (UTC+1 in September).
	start := time.Date(2026, 10, 4, 23, 30, 0, 0, time.UTC)
	sessions := []Session{lifting(start, "")}
	got := CompletedWeekdays(sessions, lisbon, time.Date(2026, 10, 5, 12, 0, 0, 0, lisbon))
	if want := []string{"Monday"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed = %v, want %v", got, want)
	}
}

func TestThisWeekListsEveryPlanDayInOrder(t *testing.T) {
	t.Parallel()

	slots := []PlanSlot{{Weekday: "Friday", Focus: "Pull"}, {Weekday: "Monday", Focus: "Push"}, {Weekday: "Wednesday", Focus: "Legs"}}
	sessions := []Session{lifting(on(0, 7), "Monday")}

	got := ThisWeek(Schedule{Default: slots}, sessions, time.UTC, on(2, 9))
	if got.Planned != 3 || got.Done != 1 {
		t.Fatalf("planned/done = %d/%d, want 3/1", got.Planned, got.Done)
	}
	var order []string
	for _, d := range got.Days {
		order = append(order, d.Weekday)
	}
	if want := []string{"Monday", "Wednesday", "Friday"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if !got.Days[0].Done || got.Days[1].Done {
		t.Fatalf("done flags = %+v", got.Days)
	}
	if got.Sentence != "1 of 3 sessions done this week." {
		t.Fatalf("sentence = %q", got.Sentence)
	}
}

func TestPlanAdherenceCountsOnlyDatesInTheWindow(t *testing.T) {
	t.Parallel()

	slots := []PlanSlot{{Weekday: "Monday"}, {Weekday: "Thursday"}}
	sessions := []Session{
		lifting(on(-7, 7), "Monday"), // last week's Monday
		lifting(on(0, 7), "Monday"),
	}
	// Two full weeks, ending before this Thursday.
	got := PlanAdherence(Schedule{Default: slots}, sessions, time.UTC, monday.AddDate(0, 0, -7), on(3, 0))
	if got.Planned != 3 || got.Done != 2 {
		t.Fatalf("planned/done = %d/%d, want 3/2", got.Planned, got.Done)
	}
	if got.Days != nil {
		t.Fatalf("a rate carries no per-day rows, got %v", got.Days)
	}
	if got.Sentence != "2 of 3 planned sessions done." {
		t.Fatalf("sentence = %q", got.Sentence)
	}
}
