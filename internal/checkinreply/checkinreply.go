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

func optional(v *int) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprint(*v)
}
