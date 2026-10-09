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

	// Stress and SleepQuality are optional 1–5 scales; nil means not given.
	Stress       *int
	SleepQuality *int
	// Tags are short lowercase labels, already normalised. Never nil once
	// stored, so clients can range without a check.
	Tags []string

	// Source is the surface the check-in was first created from. Later
	// edits, from anywhere, do not change it.
	Source Source

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Source names the surface a check-in was written from. It is an open label,
// not an enum: an unrecognised value is shown as-is rather than rejected, and
// rows from before sources were recorded read SourceUnknown.
type Source string

const (
	SourceUnknown Source = "unknown"
	SourceWeb     Source = "web"
	SourceIOS     Source = "ios"
	SourceSiri    Source = "siri"
	SourceCoach   Source = "coach"
	SourceMCP     Source = "mcp"
	SourceCapture Source = "capture"
)

// Tag limits. A tag is a label, not a sentence.
const (
	MaxTags      = 8
	MaxTagLength = 24
)

// NormalizeTags trims, lowercases and de-duplicates tags, dropping empty ones
// and keeping first-seen order. It does not enforce the limits; validation
// reports those so the person learns why a tag did not stick.
func NormalizeTags(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// Edited reports whether the entry changed notably after it was first saved.
// A save a few seconds after creation (a double tap, a retry) is not an edit.
func (c CheckIn) Edited() bool {
	return c.UpdatedAt.Sub(c.CreatedAt) > editedAfter
}

const editedAfter = 2 * time.Minute

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
	if notes := strings.TrimSpace(c.Notes); notes != "" {
		fmt.Fprintf(&b, ". Notes: %s", truncate(notes, 120))
	}
	if c.Stress != nil {
		fmt.Fprintf(&b, ". Stress %d/5", *c.Stress)
	}
	if c.SleepQuality != nil {
		fmt.Fprintf(&b, ". Sleep quality %d/5", *c.SleepQuality)
	}
	if len(c.Tags) > 0 {
		fmt.Fprintf(&b, ". Tags: %s", strings.Join(c.Tags, ", "))
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
