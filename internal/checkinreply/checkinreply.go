// Package checkinreply is the coach's short note under a saved check-in: one
// or two sentences, about what the person actually wrote, written the moment
// they save it.
//
// It is the reward for checking in that does not pay for the act of logging.
// docs/advanced-gamification.md refuses points for a check-in, because paying
// for it teaches people to type anything. A note that is only as good as what
// was written rewards writing it honestly instead.
//
// The note is decoration. Any failure — no provider, a timeout, the quota, a
// reply that breaks the rules — means no note, never an error the person sees.
package checkinreply

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
)

// Kinds, open-ended on the wire.
const (
	KindDaily = "daily"
	// KindPattern is a note that names a pattern in the person's check-ins,
	// written on the check-ins where one first has enough days to show.
	KindPattern = "pattern"
)

// patternAt are the check-in totals that look for a pattern. Sparse for the
// reason moment thresholds are: a pattern every day is noise.
var patternAt = map[int]bool{7: true, 14: true, 30: true}

// Reply is one note.
type Reply struct {
	Kind string
	Text string
}

// contentHash fingerprints what a note answers: everything the person rated
// or wrote, not when they saved it. Every save moves updated_at, so hashing
// that would pay for a new note on every unchanged re-save.
func contentHash(c checkin.CheckIn) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|%s|%s|%s|%s|%s|%s",
		c.Mood, c.Energy, optional(c.Stress), optional(c.SleepQuality),
		c.Wins, c.Challenges, c.Notes, strings.Join(c.Tags, ","))
	return hex.EncodeToString(h.Sum(nil))
}

// inFull is today's check-in with every word the person wrote. The coach's
// context shortens each field to a line (checkin.Summary); the note must not,
// because the end of a long note is where somebody may say the thing that
// matters most.
func inFull(c checkin.CheckIn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — mood %d/5, energy %d/5", c.LocalDate.Format("2 Jan"), c.Mood, c.Energy)
	if c.Stress != nil {
		fmt.Fprintf(&b, ", stress %d/5", *c.Stress)
	}
	if c.SleepQuality != nil {
		fmt.Fprintf(&b, ", sleep quality %d/5", *c.SleepQuality)
	}
	if title := strings.TrimSpace(c.RelatedGoalTitle); title != "" {
		fmt.Fprintf(&b, "\nAbout their goal: %s", title)
	}
	for _, field := range []struct{ label, text string }{
		{"Wins", c.Wins}, {"Challenges", c.Challenges}, {"Notes", c.Notes},
	} {
		if text := strings.TrimSpace(field.text); text != "" {
			fmt.Fprintf(&b, "\n%s: %s", field.label, text)
		}
	}
	if len(c.Tags) > 0 {
		fmt.Fprintf(&b, "\nTags: %s", strings.Join(c.Tags, ", "))
	}
	return b.String()
}

func optional(v *int) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprint(*v)
}
