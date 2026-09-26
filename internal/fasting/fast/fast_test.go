package fast

import (
	"testing"
	"time"
)

func TestPhaseAt(t *testing.T) {
	cases := map[time.Duration]Phase{
		2 * time.Hour:  PhaseFed,
		8 * time.Hour:  PhaseFasting,
		13 * time.Hour: PhaseFatBurning,
		20 * time.Hour: PhaseKetosis,
	}
	for d, want := range cases {
		if got := PhaseAt(d); got != want {
			t.Errorf("PhaseAt(%v) = %s, want %s", d, got, want)
		}
	}
}

func TestElapsedAndFraction(t *testing.T) {
	start := time.Date(2026, 9, 26, 2, 0, 0, 0, time.UTC)
	s := Session{StartedAt: start, TargetHours: 16}
	now := start.Add(12*time.Hour + 46*time.Minute)
	if s.Elapsed(now) != 12*time.Hour+46*time.Minute {
		t.Errorf("elapsed = %v", s.Elapsed(now))
	}
	if f := s.Fraction(now.Add(10 * time.Hour)); f != 1 {
		t.Errorf("fraction past target = %v, want capped at 1", f)
	}
	end := start.Add(16 * time.Hour)
	s.EndedAt = &end
	if s.Elapsed(now.Add(48*time.Hour)) != 16*time.Hour {
		t.Error("a finished fast stops counting at its end")
	}
}
