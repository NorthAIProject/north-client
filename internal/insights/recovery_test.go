package insights

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	"github.com/NorthAIProject/north-client/internal/users"
)

// fakeNights serves the sleep page's nights; the other stats are unused here.
type fakeNights []stat.Night

func (f fakeNights) Sleep(context.Context, users.User, timerange.Range) (stat.SleepStats, error) {
	return stat.SleepStats{Nights: f}, nil
}

func (fakeNights) Cardio(context.Context, users.User, timerange.Range) (stats.CardioStats, error) {
	return stats.CardioStats{}, nil
}

func (fakeNights) Eating(context.Context, users.User, timerange.Range) (stat.EatingStats, error) {
	return stat.EatingStats{}, nil
}

func (fakeNights) Patterns(context.Context, users.User, timerange.Range) ([]stat.Finding, int, error) {
	return nil, 0, nil
}

// A month of steady readings, then a rough night: HRV down, resting heart
// rate up, a short sleep.
func roughMorning(today time.Time) (datedHealth, fakeNights) {
	h := datedHealth{}
	h.add("hrv_sdnn", today, 29, func(back int) float64 {
		if back == 0 {
			return 38
		}
		return 55 + float64(back%5)
	})
	h.add("resting_heart_rate", today, 29, func(back int) float64 {
		if back == 0 {
			return 60
		}
		return 52 + float64(back%3)
	})
	var nights fakeNights
	for back := 28; back >= 0; back-- {
		minutes := 450 + (back%3)*15
		if back == 0 {
			minutes = 330
		}
		nights = append(nights, stat.Night{Date: today.AddDate(0, 0, -back), Minutes: minutes})
	}
	return h, nights
}

func TestRecoveryAfterARoughNight(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	h, nights := roughMorning(timerange.StartOfDay(now))
	r, err := (&Service{health: h, stats: nights}).Recovery(context.Background(), users.User{Timezone: "UTC"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Score.HasData || r.Score.Verdict() != score.VerdictLow || len(r.Signals) != 3 {
		t.Fatalf("recovery = %d %s with %d signals", r.Score.Points, r.Score.Verdict(), len(r.Signals))
	}
	for _, sig := range r.Signals {
		if usualView(sig.Metric, sig.Usual).State == stat.Usual {
			t.Errorf("%s read as usual after a rough night", sig.Key)
		}
	}

	line, ok := r.Sentence()
	if !ok || !strings.HasPrefix(line, "Recovery today: ") || !strings.Contains(line, "under your usual") ||
		!strings.Contains(line, "Sleep — Below your usual: 5.5h") || !strings.Contains(line, "Heart rate variability — Below your usual: 38ms") {
		t.Errorf("sentence = %q", line)
	}

	view := projectRecovery(r)
	if view.Verdict != "low" || view.Label != "Under your usual" || len(view.Signals) != 3 || view.Signals[2].Key != "sleep" {
		t.Errorf("view = %+v", view)
	}
}

// A watch left on the charger for a week: last week's numbers say nothing
// about this morning, and two stale signals plus nothing fresh is no score.
func TestRecoveryLeavesOutStaleReadings(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	h, nights := roughMorning(timerange.StartOfDay(now).AddDate(0, 0, -5))
	r, err := (&Service{health: h, stats: nights}).Recovery(context.Background(), users.User{Timezone: "UTC"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Score.HasData || len(r.Signals) != 0 {
		t.Errorf("stale week scored %d with %d signals", r.Score.Points, len(r.Signals))
	}
	if _, ok := r.Sentence(); ok {
		t.Error("no recovery still produced a sentence")
	}
	if view := projectRecovery(r); view.HasData || view.Verdict != "unknown" || view.Signals == nil {
		t.Errorf("view = %+v", view)
	}
}

func TestRecoveryReachesTheCoach(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	h, nights := roughMorning(timerange.StartOfDay(now))
	src := NewRecoveryContextSource(&Service{health: h, stats: nights}, func() time.Time { return now })
	var into coach.Context
	if err := src.Collect(context.Background(), coach.ContextRequest{User: users.User{Timezone: "UTC"}}, &into); err != nil {
		t.Fatal(err)
	}
	if len(into.DailySignals) != 1 || !strings.HasPrefix(into.DailySignals[0], "Recovery today: ") {
		t.Errorf("daily signals = %v", into.DailySignals)
	}

	// Nothing to say: nothing added.
	empty := NewRecoveryContextSource(&Service{}, func() time.Time { return now })
	into = coach.Context{}
	if err := empty.Collect(context.Background(), coach.ContextRequest{User: users.User{Timezone: "UTC"}}, &into); err != nil || len(into.DailySignals) != 0 {
		t.Errorf("empty source added %v, %v", into.DailySignals, err)
	}
}
