package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoundUpToIncrement(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		increment int
		want      float64
	}{
		{"rounds up to nearest 5", 27.397260, 5, 30},
		{"already a multiple is unchanged", 30, 5, 30},
		{"rounds up to nearest 10", 21, 10, 30},
		{"zero stays zero", 0, 5, 0},
		{"a hair over a multiple rounds up", 25.000001, 25, 50},
		{"increment of 1 is a plain ceiling", 27.4, 1, 28},
		{"increment of 100 collapses to 0 or 100", 0.5, 100, 100},
		{"non-positive increment is a no-op", 27.397260, 0, 27.397260},
		{"negative increment is a no-op", 27.397260, -5, 27.397260},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.want, RoundUpToIncrement(tt.value, tt.increment), 1e-9)
		})
	}
}

func TestRoundHalfUpToScale(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		scale int
		want  float64
	}{
		{"truncates a long fraction to six places", 27.397260273972602, 6, 27.39726},
		{"rounds the seventh place up", 27.3972605, 6, 27.397261},
		{"rounds the seventh place down", 27.3972604, 6, 27.39726},
		{"exact value is unchanged", 25, 6, 25},
		{"one hundred is unchanged", 100, 6, 100},
		{"zero is unchanged", 0, 6, 0},
		{"a third to six places", 33.333333333333336, 6, 33.333333},
		{"two thirds rounds up at the sixth place", 66.66666666666667, 6, 66.666667},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RoundHalfUpToScale(tt.value, tt.scale))
		})
	}
}

// The published value must be stable and short: a raw float64 would serialise as
// 27.397260273972602, which leaks arithmetic noise into the CARE/PE payload.
func TestRoundHalfUpToScale_ProducesAStableSerialisation(t *testing.T) {
	raw := 100.0 / 365.0 * 100.0

	assert.Equal(t, 27.39726, RoundHalfUpToScale(raw, 6))
}
