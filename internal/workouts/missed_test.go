package workouts_test

import (
	"context"
	"slices"
	"testing"

	"github.com/NorthAIProject/north-client/internal/workouts"
)

// The week of Monday 12 October 2026 on the Upper/Lower split: Monday Upper A,
// Tuesday Lower A, Thursday Upper B, Friday Lower B.
func TestMissedYesterdayAndMovingItToToday(t *testing.T) {
	f := newWeekFixture(t)
	ctx := context.Background()
	monday := lisbon(t, 2026, 10, 12, 8)
	// The week is recorded the first time anything reads it, as Today does
	// each morning.
	if _, err := f.svc.WeekProgress(ctx, f.user, monday); err != nil {
		t.Fatalf("week: %v", err)
	}
	f.tracker.finish(monday, "Monday")

	wednesday := lisbon(t, 2026, 10, 14, 10)
	missed, ok, err := f.svc.MissedYesterday(ctx, f.user, wednesday)
	if err != nil || !ok {
		t.Fatalf("missed = %+v %v %v, want Tuesday", missed, ok, err)
	}
	if missed.Weekday != "Tuesday" || missed.Title != "Lower A" {
		t.Errorf("missed = %+v", missed)
	}
	if !missed.TodayFree || missed.TomorrowFree {
		t.Errorf("Wednesday is free and Thursday is Upper B: %+v", missed)
	}

	if _, moveErr := f.svc.MoveMissed(ctx, f.user, wednesday, workouts.MoveTomorrow); moveErr == nil {
		t.Error("moving onto Thursday's own session should be refused")
	}

	week, err := f.svc.MoveMissed(ctx, f.user, wednesday, workouts.MoveToday)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	want := []string{"Monday Upper A", "Wednesday Lower A", "Thursday Upper B", "Friday Lower B"}
	if got := sessions(week); !slices.Equal(got, want) {
		t.Errorf("week = %v, want %v: the rest of the week as it was, Lower A once", got, want)
	}

	// Moved, it is no longer missed: the coach does not ask twice.
	if _, ok, _ := f.svc.MissedYesterday(ctx, f.user, wednesday); ok {
		t.Error("a moved session is still reported missed")
	}
}

func TestNothingMissedAcrossTheWeekBoundaryOrWhenDone(t *testing.T) {
	f := newWeekFixture(t)
	ctx := context.Background()

	// Monday: yesterday was last week, which the rotation already carried.
	monday := lisbon(t, 2026, 10, 19, 9)
	if _, ok, err := f.svc.MissedYesterday(ctx, f.user, monday); err != nil || ok {
		t.Errorf("Monday: ok %v err %v, want nothing to move", ok, err)
	}

	// Tuesday after a trained Monday.
	weekStart := lisbon(t, 2026, 10, 12, 8)
	if _, err := f.svc.WeekProgress(ctx, f.user, weekStart); err != nil {
		t.Fatalf("week: %v", err)
	}
	f.tracker.finish(weekStart, "Monday")
	if _, ok, _ := f.svc.MissedYesterday(ctx, f.user, lisbon(t, 2026, 10, 13, 9)); ok {
		t.Error("a trained Monday is not missed")
	}
	// Thursday after a rest Wednesday.
	if _, ok, _ := f.svc.MissedYesterday(ctx, f.user, lisbon(t, 2026, 10, 15, 9)); ok {
		t.Error("a rest day is not missed")
	}
}
