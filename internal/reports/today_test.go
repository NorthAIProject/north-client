package reports

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/users"
)

// morning is a Health store: two weeks of HRV around 60 ms (±4) and resting
// heart rate around 52 bpm (±2), then this morning's values. A zero leaves
// that metric out.
type morning struct {
	at             time.Time
	hrv, restingHR float64
}

func (m morning) Between(_ context.Context, _ uuid.UUID, metric string, _, _ time.Time) ([]health.Stored, error) {
	base, spread, today := 60.0, 4.0, m.hrv
	if metric == "resting_heart_rate" {
		base, spread, today = 52, 2, m.restingHR
	}
	if today == 0 {
		return nil, nil
	}
	out := []health.Stored{{Value: today, StartedAt: m.at.Add(-2 * time.Hour)}}
	for d := 1; d <= 14; d++ {
		out = append(out, health.Stored{Value: base + spread*float64(d%3-1), StartedAt: m.at.AddDate(0, 0, -d)})
	}
	return out, nil
}

type failing struct{}

func (failing) Recovery(context.Context, users.User, time.Time) (insights.RecoveryData, error) {
	return insights.RecoveryData{}, errors.New("health is down")
}

// Readiness is decided in code, by the same recovery rule as the lighter-day
// offer, so the briefing can only repeat it.
func TestReadinessIsTheRecoveryRule(t *testing.T) {
	now := time.Date(2026, 9, 30, 7, 0, 0, 0, time.UTC)
	user := users.User{Timezone: "UTC"}
	cases := []struct {
		name    string
		reader  Recovery
		verdict string
	}{
		{"steady", insights.NewRecoverySource(morning{at: now, hrv: 58, restingHR: 53}, nil), "Readiness: normal."},
		{"HRV down, resting HR up", insights.NewRecoverySource(morning{at: now, hrv: 48, restingHR: 57}, nil), "Readiness: LOW"},
		{"one signal is not enough", insights.NewRecoverySource(morning{at: now, hrv: 48}, nil), ""},
		{"a reader that fails costs only its lines", failing{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := NewTodayContext(tc.reader, nil, nil).Load(context.Background(), user, now)
			if tc.verdict == "" {
				if len(lines) != 0 {
					t.Fatalf("lines = %q, want none", lines)
				}
				return
			}
			if len(lines) != 2 || !strings.HasPrefix(lines[0], "Recovery today: ") || !strings.HasPrefix(lines[1], tc.verdict) {
				t.Fatalf("lines = %q, want a recovery line then %q", lines, tc.verdict)
			}
		})
	}
}

// Only the daily briefing gets a Today section, and it comes first.
func TestTodaySectionIsDailyOnly(t *testing.T) {
	review := ReviewContext{Today: []string{"Readiness: LOW — recovery is under this person's usual this morning."}}
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
