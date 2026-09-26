package fitness

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/health"
)

func completed(code string, end time.Time, minutes int, distanceM float64) activity.Session {
	s := activity.Session{
		ActivityCode: code,
		Status:       activity.StatusCompleted,
		StartedAt:    end.Add(-time.Duration(minutes) * time.Minute),
		EndedAt:      &end,
	}
	if distanceM > 0 {
		s.DistanceM = &distanceM
	}
	return s
}

func TestBuildWeekSumsTheTrailingSevenDays(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, loc)

	week := buildWeek(loc, now, []activity.Session{
		completed("walking_brisk", now.Add(-time.Hour), 60, 5000),
		completed("strength_training", now.AddDate(0, 0, -2), 45, 0),
		// Eight days back: outside the window.
		completed("running_8kmh", now.AddDate(0, 0, -8), 30, 5000),
		// Still running: not a finished session, so not counted.
		{ActivityCode: "cycling_leisure", Status: activity.StatusActive, StartedAt: now},
	})

	if len(week.Days) != 7 {
		t.Fatalf("days = %d, want 7", len(week.Days))
	}
	if !week.Days[6].IsToday || week.Days[0].IsToday {
		t.Fatal("only the last day should be today")
	}
	if week.Sessions != 2 {
		t.Fatalf("sessions = %d, want 2", week.Sessions)
	}
	if week.Minutes != 105 {
		t.Fatalf("minutes = %v, want 105", week.Minutes)
	}
	if week.DistanceKm != 5 {
		t.Fatalf("distance = %v, want 5", week.DistanceKm)
	}
	if week.Days[6].Minutes != 60 || week.Days[4].Minutes != 45 {
		t.Fatalf("per-day minutes = %+v", week.Days)
	}
}

func TestBuildRecentKeepsNewestCompletedAndNamesThem(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	var sessions []activity.Session
	sessions = append(sessions, activity.Session{ActivityCode: "hiit", Status: activity.StatusCancelled, StartedAt: now})
	for i := range 7 {
		sessions = append(sessions, completed("walking_brisk", now.Add(-time.Duration(i)*time.Hour), 30, 0))
	}

	recent := buildRecent(sessions)
	if len(recent) != recentLimit {
		t.Fatalf("recent = %d, want %d", len(recent), recentLimit)
	}
	if recent[0].Name != "Walking (brisk pace)" || recent[0].Category != "cardio" {
		t.Fatalf("first = %+v", recent[0])
	}
	if recent[0].Duration != 30*time.Minute {
		t.Fatalf("duration = %v", recent[0].Duration)
	}
}

func TestDailyTotalsFillsGapsWithZero(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, loc)
	readings := []health.Stored{
		{Value: 4000, StartedAt: now.Add(-time.Hour)},
		{Value: 2500, StartedAt: now.Add(-2 * time.Hour)},
		{Value: 9000, StartedAt: now.AddDate(0, 0, -3)},
	}

	got := dailyTotals(loc, now, 5, readings)
	want := []float64{0, 9000, 0, 0, 6500}
	for i, w := range want {
		if got[i].Value != w {
			t.Fatalf("day %d = %v, want %v (all: %+v)", i, got[i].Value, w, got)
		}
	}
}

func TestDailyMeansAveragesAndOrdersOldestFirst(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, loc)
	// Newest first, as the repository returns them.
	readings := []health.Stored{
		{Value: 42, StartedAt: now},
		{Value: 40, StartedAt: now.Add(-time.Hour)},
		{Value: 44, StartedAt: now.AddDate(0, 0, -5)},
	}

	got := dailyMeans(loc, readings)
	if len(got) != 2 {
		t.Fatalf("days = %d, want 2", len(got))
	}
	if got[0].Value != 44 || got[1].Value != 41 {
		t.Fatalf("means = %+v", got)
	}
}

func TestLatestWeightReportsChangeFromPrevious(t *testing.T) {
	w, ok := latestWeight([]biometrics.Biometric{{WeightKg: 80.5}, {WeightKg: 82}})
	if !ok || w.Kg != 80.5 || !w.HasDelta || w.DeltaKg != -1.5 {
		t.Fatalf("weight = %+v ok=%v", w, ok)
	}

	if _, ok := latestWeight(nil); ok {
		t.Fatal("no history should report no weight")
	}

	single, _ := latestWeight([]biometrics.Biometric{{WeightKg: 70}})
	if single.HasDelta {
		t.Fatal("a single weigh-in has nothing to compare against")
	}
}

func TestTrendViewDropsTrailingEmptyDaysAndEmptySeries(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }

	got := trendView("x", "var(--north-signal)", []DailyValue{{day(24), 5000}, {day(25), 6000}, {day(26), 0}})
	if got == nil || len(got.Days) != 2 {
		t.Fatalf("trend = %+v, want two days", got)
	}

	if trendView("x", "var(--north-signal)", []DailyValue{{day(25), 0}, {day(26), 0}}) != nil {
		t.Fatal("an all-empty series should draw no card")
	}
}
