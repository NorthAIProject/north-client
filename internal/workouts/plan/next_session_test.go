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
	got, ok := p.NextSession(wednesday)
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
	got, ok := p.NextSession(thursday)
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
	got, ok := p.NextSession(saturday)
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
	got, ok := p.NextSession(wednesday)
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
	if _, ok := p.NextSession(time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)); ok {
		t.Fatal("unrecognised weekdays should yield no session")
	}
}
