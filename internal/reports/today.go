package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The morning briefing's view of the day ahead: how recovered the body looks,
// what training is planned, and what the calendar holds. The weekly review has
// no use for any of it, so only the daily briefing reads it.

// Metric names as the phone syncs them; see internal/day for the same list.
const (
	metricHRV       = "hrv_sdnn"
	metricRestingHR = "resting_heart_rate"
)

// readinessBaselineDays is the window a morning is compared against. Two weeks
// smooths out one bad night without hiding a trend that has lasted a week.
const readinessBaselineDays = 14

// Thresholds for calling a morning low. HRV below its baseline and resting
// heart rate above its baseline are the two signs of an under-recovered body
// that the phone reliably has; the margins are wide enough that ordinary
// day-to-day noise does not trip them.
const (
	lowHRVDrop        = 0.10
	highRestingHRRise = 0.05
)

type HealthReader interface {
	Between(ctx context.Context, userID uuid.UUID, metric string, since, until time.Time) ([]health.Stored, error)
}

type SessionReader interface {
	DueToday(ctx context.Context, user users.User, today time.Time) (string, string, bool, error)
	CompletedToday(ctx context.Context, user users.User, today time.Time) (bool, string, error)
}

type CalendarReader interface {
	Upcoming(ctx context.Context, userID uuid.UUID) ([]string, error)
}

// TodayContext loads the "Today" section. Any reader may be nil, and a reader
// that fails costs only its own lines: a calendar that cannot be reached must
// not cost somebody their briefing.
type TodayContext struct {
	health   HealthReader
	sessions SessionReader
	calendar CalendarReader
}

func NewTodayContext(h HealthReader, s SessionReader, c CalendarReader) *TodayContext {
	return &TodayContext{health: h, sessions: s, calendar: c}
}

// maxCalendarLines keeps the briefing about today rather than a week's diary.
const maxCalendarLines = 6

func (t *TodayContext) Load(ctx context.Context, user users.User, now time.Time) []string {
	var lines []string
	if t.health != nil {
		lines = append(lines, t.readiness(ctx, user, now)...)
	}
	if t.sessions != nil {
		lines = append(lines, t.session(ctx, user, now)...)
	}
	if t.calendar != nil {
		if upcoming, err := t.calendar.Upcoming(ctx, user.ID); err == nil {
			for i, line := range upcoming {
				if i == maxCalendarLines {
					break
				}
				lines = append(lines, "Calendar: "+line)
			}
		}
	}
	return lines
}

// Readiness is the verdict the briefing may repeat. It is decided here, from
// numbers, so the model never has to judge recovery on its own.
type Readiness struct {
	HRV, HRVBaseline float64
	RHR, RHRBaseline float64
	HasHRV, HasRHR   bool
	Low              bool
}

func (t *TodayContext) readiness(ctx context.Context, user users.User, now time.Time) []string {
	since := now.AddDate(0, 0, -readinessBaselineDays)
	hrv, errHRV := t.health.Between(ctx, user.ID, metricHRV, since, now)
	rhr, errRHR := t.health.Between(ctx, user.ID, metricRestingHR, since, now)
	if errHRV != nil {
		hrv = nil
	}
	if errRHR != nil {
		rhr = nil
	}
	r := ReadinessFrom(hrv, rhr, now)
	return r.Lines()
}

// ReadinessFrom compares the last day's readings with the ones before it.
func ReadinessFrom(hrv, rhr []health.Stored, now time.Time) Readiness {
	var r Readiness
	r.HRV, r.HRVBaseline, r.HasHRV = latestAgainstBaseline(hrv, now)
	r.RHR, r.RHRBaseline, r.HasRHR = latestAgainstBaseline(rhr, now)
	if r.HasHRV && r.HRV < r.HRVBaseline*(1-lowHRVDrop) {
		r.Low = true
	}
	if r.HasRHR && r.RHR > r.RHRBaseline*(1+highRestingHRRise) {
		r.Low = true
	}
	return r
}

// latestAgainstBaseline is the newest reading of the last 24 hours and the
// mean of everything older in the window. No reading today, or nothing to
// compare it with, is no claim at all.
func latestAgainstBaseline(samples []health.Stored, now time.Time) (latest, baseline float64, ok bool) {
	dayAgo := now.Add(-24 * time.Hour)
	var latestAt time.Time
	var sum float64
	var n int
	for _, s := range samples {
		if s.StartedAt.After(dayAgo) {
			if s.StartedAt.After(latestAt) {
				latest, latestAt = s.Value, s.StartedAt
			}
			continue
		}
		sum += s.Value
		n++
	}
	if latestAt.IsZero() || n == 0 {
		return 0, 0, false
	}
	return latest, sum / float64(n), true
}

func (r Readiness) Lines() []string {
	var lines []string
	if r.HasHRV {
		lines = append(lines, fmt.Sprintf("HRV this morning: %.0f ms (%d-day average %.0f ms, %+.0f%%)",
			r.HRV, readinessBaselineDays, r.HRVBaseline, pctChange(r.HRV, r.HRVBaseline)))
	}
	if r.HasRHR {
		lines = append(lines, fmt.Sprintf("Resting heart rate: %.0f bpm (%d-day average %.0f bpm, %+.0f%%)",
			r.RHR, readinessBaselineDays, r.RHRBaseline, pctChange(r.RHR, r.RHRBaseline)))
	}
	switch {
	case !r.HasHRV && !r.HasRHR:
		return nil
	case r.Low:
		lines = append(lines, "Readiness: LOW — recovery markers are off their baseline this morning.")
	default:
		lines = append(lines, "Readiness: normal.")
	}
	return lines
}

func pctChange(value, base float64) float64 {
	if base == 0 {
		return 0
	}
	return (value - base) / base * 100
}

func (t *TodayContext) session(ctx context.Context, user users.User, now time.Time) []string {
	today := now.In(user.Location())
	title, _, due, err := t.sessions.DueToday(ctx, user, today)
	if err != nil || !due {
		return []string{"Training: nothing scheduled today."}
	}
	done, _, err := t.sessions.CompletedToday(ctx, user, today)
	if err == nil && done {
		return []string{fmt.Sprintf("Training: today's session (%s) is already done.", title)}
	}
	return []string{fmt.Sprintf("Training: %s is scheduled today.", title)}
}
