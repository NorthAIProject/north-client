// Package checkin holds the shape of a daily reflection.
//
// Leaf package so templates and handlers can import it without cycles.
package checkin

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CheckIn is one day's reflection.
type CheckIn struct {
	ID     uuid.UUID
	UserID uuid.UUID

	// LocalDate is the calendar day in the user's timezone (time at midnight local).
	LocalDate time.Time

	Mood   int // 1–5
	Energy int // 1–5

	Wins       string
	Challenges string
	Notes      string

	RelatedGoalID    *uuid.UUID
	RelatedGoalTitle string // filled when listing with a goal join/lookup

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Summary is the coach-facing one-liner.
func (c CheckIn) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — mood %d/5, energy %d/5",
		c.LocalDate.Format("2 Jan"), c.Mood, c.Energy)
	if title := strings.TrimSpace(c.RelatedGoalTitle); title != "" {
		fmt.Fprintf(&b, " (re: %s)", title)
	}
	if wins := strings.TrimSpace(c.Wins); wins != "" {
		fmt.Fprintf(&b, ". Wins: %s", truncate(wins, 120))
	}
	if challenges := strings.TrimSpace(c.Challenges); challenges != "" {
		fmt.Fprintf(&b, ". Challenges: %s", truncate(challenges, 120))
	}
	return b.String()
}

// Label is a short list heading.
func (c CheckIn) Label() string {
	return fmt.Sprintf("%s — mood %d · energy %d",
		c.LocalDate.Format("Mon 2 Jan"), c.Mood, c.Energy)
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
