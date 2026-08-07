package habit_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/habits/habit"
)

// A fixed week to reason against, so "Tuesday" means the same thing in every
// test. 2026-08-03 is a Monday.
var (
	mon = date(2026, 8, 3)
	tue = date(2026, 8, 4)
	wed = date(2026, 8, 5)
	thu = date(2026, 8, 6)
	fri = date(2026, 8, 7)
	sat = date(2026, 8, 8)
	sun = date(2026, 8, 9)
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func mwf() habit.Habit {
	return habit.Habit{
		Name:   "Lift",
		Domain: "fitness",
		Days:   []time.Weekday{time.Monday, time.Wednesday, time.Friday},
		Active: true,
	}
}

func daily() habit.Habit {
	return habit.Habit{
		Name:   "Meditate",
		Domain: "personal",
		Days: []time.Weekday{
			time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
			time.Thursday, time.Friday, time.Saturday,
		},
		Active: true,
	}
}

func TestSanityOfTheFixtureWeek(t *testing.T) {
	t.Parallel()

	if mon.Weekday() != time.Monday || fri.Weekday() != time.Friday {
		t.Fatalf("fixture dates are not the weekdays they claim: %s is %s", mon, mon.Weekday())
	}
}

// The rule this whole slice exists to get right: an unscheduled day is not a
// missed day.
func TestStreakSurvivesDaysTheHabitIsNotDueOn(t *testing.T) {
	t.Parallel()

	// Kept every scheduled day of the week. "Today" is the Sunday after: two
	// unscheduled days (Sat, Sun) sit between it and the last kept Friday,
	// and Tue/Thu sit inside the run.
	completed := habit.NewDateSet([]time.Time{mon, wed, fri})

	if got := habit.Streak(mwf(), completed, sun); got != 3 {
		t.Errorf("Streak = %d, want 3 — Tue, Thu, Sat and Sun must not break it", got)
	}
}

// The mirror of the test above, and the reason it is easy to get wrong: a
// gap of unscheduled days is forgiven, but a scheduled day inside the gap is
// not. Here the following Monday is due and was skipped, so a Tuesday asking
// about the streak sees it broken however good the week before was.
func TestAScheduledDayInsideTheGapStillBreaksTheStreak(t *testing.T) {
	t.Parallel()

	completed := habit.NewDateSet([]time.Time{mon, wed, fri})
	nextMon := date(2026, 8, 10)
	nextTue := date(2026, 8, 11)

	if nextMon.Weekday() != time.Monday {
		t.Fatalf("fixture: %s is %s, want Monday", nextMon, nextMon.Weekday())
	}

	if got := habit.Streak(mwf(), completed, nextTue); got != 0 {
		t.Errorf("Streak = %d, want 0 — the Monday between was due and missed", got)
	}
}

func TestStreakBreaksOnAMissedScheduledDay(t *testing.T) {
	t.Parallel()

	// Wednesday skipped.
	completed := habit.NewDateSet([]time.Time{mon, fri})

	if got := habit.Streak(mwf(), completed, fri); got != 1 {
		t.Errorf("Streak = %d, want 1 — only Friday survives a missed Wednesday", got)
	}
}

// Today is not over. A habit due tonight should not read as broken this
// morning.
func TestTodayIsForgivenUntilItIsOver(t *testing.T) {
	t.Parallel()

	completed := habit.NewDateSet([]time.Time{mon, wed})

	// Friday is scheduled and not yet done; the streak stands at Mon+Wed.
	if got := habit.Streak(mwf(), completed, fri); got != 2 {
		t.Errorf("Streak on an unfinished scheduled day = %d, want 2", got)
	}

	// Once Friday is kept it counts.
	withFriday := habit.NewDateSet([]time.Time{mon, wed, fri})
	if got := habit.Streak(mwf(), withFriday, fri); got != 3 {
		t.Errorf("Streak after keeping today = %d, want 3", got)
	}
}

func TestStreakIsZeroWhenNothingWasEverKept(t *testing.T) {
	t.Parallel()

	if got := habit.Streak(mwf(), habit.NewDateSet(nil), fri); got != 0 {
		t.Errorf("Streak = %d, want 0", got)
	}
}

func TestStreakOfADailyHabitCountsEveryDay(t *testing.T) {
	t.Parallel()

	completed := habit.NewDateSet([]time.Time{mon, tue, wed, thu, fri})

	if got := habit.Streak(daily(), completed, fri); got != 5 {
		t.Errorf("Streak = %d, want 5", got)
	}
}

func TestAdherenceCountsOnlyScheduledDays(t *testing.T) {
	t.Parallel()

	// Window: the seven days ending Sunday. Scheduled: Mon, Wed, Fri. Kept
	// two of them.
	completed := habit.NewDateSet([]time.Time{mon, fri})

	kept, scheduled := habit.Adherence(mwf(), completed, sun, 7)
	if scheduled != 3 {
		t.Errorf("scheduled = %d, want 3 — Tue/Thu/Sat/Sun are not due days", scheduled)
	}
	if kept != 2 {
		t.Errorf("kept = %d, want 2", kept)
	}

	stats := habit.Stats{Habit: mwf(), Kept: kept, Scheduled: scheduled}
	if stats.Rate() != 66 {
		t.Errorf("Rate = %d, want 66", stats.Rate())
	}
}

// Same forgiveness as the streak, so a person is not marked down at breakfast
// for a habit they do after work.
func TestAdherenceExcludesAnUnfinishedToday(t *testing.T) {
	t.Parallel()

	completed := habit.NewDateSet([]time.Time{mon, wed})

	// Today is Friday, scheduled, not yet kept: it should leave the
	// denominator rather than count against them.
	kept, scheduled := habit.Adherence(mwf(), completed, fri, 7)
	if kept != 2 || scheduled != 2 {
		t.Errorf("kept/scheduled = %d/%d, want 2/2", kept, scheduled)
	}

	// Once kept, it joins both sides.
	withFriday := habit.NewDateSet([]time.Time{mon, wed, fri})
	kept, scheduled = habit.Adherence(mwf(), withFriday, fri, 7)
	if kept != 3 || scheduled != 3 {
		t.Errorf("kept/scheduled after keeping today = %d/%d, want 3/3", kept, scheduled)
	}
}

func TestRateOfAWindowWithNothingDueIsNotAFailure(t *testing.T) {
	t.Parallel()

	// A Sunday-only habit, asked about a two-day window of Mon-Tue.
	sundayOnly := habit.Habit{Days: []time.Weekday{time.Sunday}, Active: true}

	kept, scheduled := habit.Adherence(sundayOnly, habit.NewDateSet(nil), tue, 2)
	if kept != 0 || scheduled != 0 {
		t.Errorf("kept/scheduled = %d/%d, want 0/0", kept, scheduled)
	}

	stats := habit.Stats{Habit: sundayOnly, Kept: kept, Scheduled: scheduled}
	if stats.Rate() != 100 {
		t.Errorf("Rate = %d, want 100 — nothing was asked for, so nothing was missed", stats.Rate())
	}
}

func TestScheduleRendersInWeekOrderRegardlessOfStorageOrder(t *testing.T) {
	t.Parallel()

	scrambled := habit.Habit{Days: []time.Weekday{time.Friday, time.Monday, time.Wednesday}}
	if got := scrambled.Schedule(); got != "Mon, Wed, Fri" {
		t.Errorf("Schedule = %q, want %q", got, "Mon, Wed, Fri")
	}

	if got := daily().Schedule(); got != "Every day" {
		t.Errorf("Schedule = %q, want %q", got, "Every day")
	}
}

func TestAHabitWithNoScheduledDaysCannotStreak(t *testing.T) {
	t.Parallel()

	none := habit.Habit{Days: nil}
	if got := habit.Streak(none, habit.NewDateSet([]time.Time{mon}), mon); got != 0 {
		t.Errorf("Streak = %d, want 0", got)
	}
}
