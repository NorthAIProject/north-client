// Package sleep holds the shape of one night's recorded sleep.
//
// A leaf, so the sleep service and any template that renders a night do not
// import each other. See CLAUDE.md on slice layout.
package sleep

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Log is one night, recorded against the morning it ended.
//
// Duration is stored in minutes rather than a start/end pair because that is
// the thing every downstream question actually asks ("did they get enough?"),
// and because bedtime and wake time are frequently approximate while the
// total is not.
type Log struct {
	ID     uuid.UUID
	UserID uuid.UUID

	// LocalDate is the morning they woke up: the day the sleep counts toward.
	LocalDate time.Time

	DurationMinutes int

	// Quality is 1-5, matching check-ins' mood scale. Nil when they logged
	// hours without rating the night, which is the common case.
	Quality *int

	// Bedtime and WakeTime are "HH:MM" or empty. Optional context, not the
	// source of Duration — someone who was awake for an hour at 3am should
	// not have that hour counted as sleep.
	Bedtime  string
	WakeTime string

	Notes string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Hours is the night's duration as a decimal, for display.
func (l Log) Hours() float64 { return float64(l.DurationMinutes) / 60 }

// Duration renders the night as "7h 30m".
func (l Log) Duration() string {
	h := l.DurationMinutes / 60
	m := l.DurationMinutes % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// Summary renders a night for the coach's context.
func (l Log) Summary() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s: %s", l.LocalDate.Format("2 Jan"), l.Duration())
	if l.Quality != nil {
		fmt.Fprintf(&b, ", quality %d/5", *l.Quality)
	}
	if l.Bedtime != "" && l.WakeTime != "" {
		fmt.Fprintf(&b, " (%s-%s)", l.Bedtime, l.WakeTime)
	}
	if notes := strings.TrimSpace(l.Notes); notes != "" {
		fmt.Fprintf(&b, " — %s", truncate(notes, 120))
	}

	return b.String()
}

// Trend is an average over a run of recorded nights.
type Trend struct {
	AverageMinutes float64
	AverageQuality float64

	// QualityCount is how many of the nights carried a rating, since quality
	// is optional. An average over two of nine nights is a different claim
	// from an average over nine.
	QualityCount int
	Nights       int
}

func (t Trend) AverageHours() float64 { return t.AverageMinutes / 60 }

// Summary renders the trend for the coach's context.
func (t Trend) Summary() string {
	if t.Nights == 0 {
		return "Sleep: nothing logged yet"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Sleep: averaging %.1fh over the last %d night", t.AverageHours(), t.Nights)
	if t.Nights != 1 {
		b.WriteString("s")
	}
	if t.QualityCount > 0 {
		fmt.Fprintf(&b, ", quality %.1f/5 across %d rated", t.AverageQuality, t.QualityCount)
	}

	return b.String()
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
