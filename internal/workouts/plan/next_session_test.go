package plan

import (
	"testing"
	"time"
)

func TestNextSessionPrefersTodayWhenItIsAPlanDay(t *testing.T) {
	t.Parallel()

	p := planWith(
		day("Monday", exercise("Squat", "barbell")),
		day("Wednesday", exercise("Push-up", "none")),
		day("Friday", exercise("Row", "dumbbell")),
	)

	wednesday := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	got, ok := p.NextSession(wednesday, nil)
	if !ok {
		t.Fatal("expected a session")
	}
	if got.Weekday != "Wednesday" {
		t.Fatalf("weekday = %q, want Wednesday", got.Weekday)
	}
}

func TestNextSessionWalksForwardToTheNextPlanDay(t *testing.T) {
	t.Parallel()

	p := planWith(
		day("Monday", exercise("Squat", "barbell")),
		day("Friday", exercise("Row", "dumbbell")),
	)

	thursday := time.Date(2026, 8, 13, 18, 0, 0, 0, time.UTC)
	got, ok := p.NextSession(thursday, nil)
	if !ok {
		t.Fatal("expected a session")
	}
	if got.Weekday != "Friday" {
		t.Fatalf("weekday = %q, want Friday", got.Weekday)
	}
}

func TestNextSessionWrapsToTheFollowingWeek(t *testing.T) {
	t.Parallel()

	p := planWith(day("Monday", exercise("Squat", "barbell")))

	saturday := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	got, ok := p.NextSession(saturday, nil)
	if !ok {
		t.Fatal("expected a session")
	}
	if got.Weekday != "Monday" {
		t.Fatalf("weekday = %q, want Monday", got.Weekday)
	}
}

func TestNextSessionIgnoresUnknownWeekdayLabels(t *testing.T) {
	t.Parallel()

	p := planWith(
		day("Leg day", exercise("Squat", "barbell")),
		day("thursday", exercise("Push-up", "none")),
	)

	wednesday := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	got, ok := p.NextSession(wednesday, nil)
	if !ok {
		t.Fatal("expected a session")
	}
	if got.Weekday != "thursday" {
		t.Fatalf("weekday = %q, want thursday", got.Weekday)
	}
}

func TestNextSessionEmptyWhenNoParseableDays(t *testing.T) {
	t.Parallel()

	p := planWith(day("whenever", exercise("Walk", "none")))
	if _, ok := p.NextSession(time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC), nil); ok {
		t.Fatal("unrecognised weekdays should yield no session")
	}
}

func TestNextSessionSkipsTodayOnceItIsDone(t *testing.T) {
	t.Parallel()

	p := planWith(
		day("Monday", exercise("Squat", "barbell")),
		day("Wednesday", exercise("Push-up", "none")),
		day("Friday", exercise("Row", "dumbbell")),
	)

	wednesday := time.Date(2026, 8, 12, 19, 0, 0, 0, time.UTC)
	got, ok := p.NextSession(wednesday, []string{"Monday", "Wednesday"})
	if !ok {
		t.Fatal("expected a session")
	}
	if got.Weekday != "Friday" {
		t.Fatalf("weekday = %q, want Friday", got.Weekday)
	}
}

func TestNextSessionSkipsADayTrainedEarly(t *testing.T) {
	t.Parallel()

	p := planWith(
		day("Tuesday", exercise("Squat", "barbell")),
		day("Wednesday", exercise("Push-up", "none")),
		day("Friday", exercise("Row", "dumbbell")),
	)

	// Wednesday's workout was done on Monday; Tuesday is still open.
	monday := time.Date(2026, 8, 10, 19, 0, 0, 0, time.UTC)
	got, _ := p.NextSession(monday, []string{"Wednesday"})
	if got.Weekday != "Tuesday" {
		t.Fatalf("weekday = %q, want Tuesday", got.Weekday)
	}
	tuesday := monday.AddDate(0, 0, 1)
	got, _ = p.NextSession(tuesday, []string{"Tuesday", "Wednesday"})
	if got.Weekday != "Friday" {
		t.Fatalf("weekday = %q, want Friday", got.Weekday)
	}
}

func TestNextSessionWrapsPastAFinishedWeek(t *testing.T) {
	t.Parallel()

	p := planWith(
		day("Monday", exercise("Squat", "barbell")),
		day("Saturday", exercise("Row", "dumbbell")),
	)

	// Everything this week is done: next week's Monday is next, even though
	// this week's Monday is in the finished list.
	saturday := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	got, ok := p.NextSession(saturday, []string{"Monday", "Saturday"})
	if !ok {
		t.Fatal("expected a session")
	}
	if got.Weekday != "Monday" {
		t.Fatalf("weekday = %q, want Monday", got.Weekday)
	}
}
