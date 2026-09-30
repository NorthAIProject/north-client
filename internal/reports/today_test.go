package reports

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/users"
)

func samples(now time.Time, today float64, baseline ...float64) []health.Stored {
	out := []health.Stored{{Value: today, StartedAt: now.Add(-2 * time.Hour)}}
	for i, v := range baseline {
		out = append(out, health.Stored{Value: v, StartedAt: now.AddDate(0, 0, -(i + 2))})
	}
	return out
}

// Readiness is decided in code so the briefing can only repeat it.
func TestReadiness(t *testing.T) {
	now := time.Date(2026, 9, 30, 7, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		hrv, rhr []health.Stored
		low      bool
		lines    int
	}{
		{"steady", samples(now, 58, 60, 60), samples(now, 52, 52, 52), false, 3},
		{"HRV down 15%", samples(now, 51, 60, 60), samples(now, 52, 52, 52), true, 3},
		{"HRV down 5% is noise", samples(now, 57, 60, 60), nil, false, 2},
		{"resting HR up 8%", nil, samples(now, 56, 52, 52), true, 2},
		{"nothing this morning", []health.Stored{{Value: 60, StartedAt: now.AddDate(0, 0, -3)}}, nil, false, 0},
		{"no history to compare", []health.Stored{{Value: 40, StartedAt: now.Add(-time.Hour)}}, nil, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ReadinessFrom(tc.hrv, tc.rhr, now)
			if r.Low != tc.low {
				t.Fatalf("low = %v, want %v (%+v)", r.Low, tc.low, r)
			}
			lines := r.Lines()
			if len(lines) != tc.lines {
				t.Fatalf("lines = %q, want %d", lines, tc.lines)
			}
			if tc.low && !strings.Contains(lines[len(lines)-1], "LOW") {
				t.Fatalf("verdict line = %q", lines[len(lines)-1])
			}
		})
	}
}

// Only the daily briefing gets a Today section, and it comes first.
func TestTodaySectionIsDailyOnly(t *testing.T) {
	review := ReviewContext{Today: []string{"Readiness: LOW — recovery markers are off their baseline this morning."}}
	user := users.User{DisplayName: "Ana", Timezone: "Europe/Lisbon"}
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	daily := formatContext(user, Report{Kind: KindDaily, PeriodStart: start}, review)
	if !strings.Contains(daily, "Today:\n- Readiness: LOW") || strings.Index(daily, "Today:") > strings.Index(daily, "Goals:") {
		t.Fatalf("daily context:\n%s", daily)
	}
	weekly := formatContext(user, Report{Kind: KindWeekly, PeriodStart: start, PeriodEnd: start.AddDate(0, 0, 7)}, review)
	if strings.Contains(weekly, "Today:") {
		t.Fatalf("weekly context has a Today section:\n%s", weekly)
	}
}
