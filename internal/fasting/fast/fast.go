// Package fast holds the shape of a fasting window and what its length means.
package fast

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Session is one fast: open until EndedAt is set.
type Session struct {
	ID          uuid.UUID
	StartedAt   time.Time
	EndedAt     *time.Time
	TargetHours int
}

// Open reports whether the fast is still going.
func (s Session) Open() bool { return s.EndedAt == nil }

// Elapsed is how long the fast has run, up to now if it is still open.
func (s Session) Elapsed(now time.Time) time.Duration {
	end := now
	if s.EndedAt != nil {
		end = *s.EndedAt
	}
	if end.Before(s.StartedAt) {
		return 0
	}
	return end.Sub(s.StartedAt)
}

// Fraction is progress toward the target, capped at 1.
func (s Session) Fraction(now time.Time) float64 {
	if s.TargetHours <= 0 {
		return 0
	}
	return min(s.Elapsed(now).Hours()/float64(s.TargetHours), 1)
}

// Phase names the metabolic stage a fast has reached.
type Phase string

const (
	PhaseFed        Phase = "fed"
	PhaseFasting    Phase = "fasting"
	PhaseFatBurning Phase = "fat_burning"
	PhaseKetosis    Phase = "ketosis"
)

// PhaseAt places an elapsed fast in its stage.
//
// The thresholds are the conventional ones fasting apps show — glycogen
// running low around twelve hours, ketosis beginning near eighteen — and are
// labels for orientation, not measurements of anyone's metabolism.
func PhaseAt(elapsed time.Duration) Phase {
	switch h := elapsed.Hours(); {
	case h < 4:
		return PhaseFed
	case h < 12:
		return PhaseFasting
	case h < 18:
		return PhaseFatBurning
	default:
		return PhaseKetosis
	}
}

// Summary renders a fast for the coach.
func (s Session) Summary(now time.Time) string {
	e := s.Elapsed(now)
	h, m := int(e.Hours()), int(e.Minutes())%60
	if s.Open() {
		return fmt.Sprintf("Fasting: %dh %dm into a %dh fast (started %s).", h, m, s.TargetHours, s.StartedAt.Format("Mon 15:04"))
	}
	return fmt.Sprintf("Fasting: last fast lasted %dh %dm of a %dh target, ended %s.", h, m, s.TargetHours, s.EndedAt.Format("Mon 15:04"))
}
