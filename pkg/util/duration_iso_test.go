package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDurationToISO8601(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected string
	}{
		{name: "zero", input: 0, expected: "P0D"},
		{name: "minutes only", input: 30 * time.Minute, expected: "PT30M"},
		{name: "hours and minutes", input: 90 * time.Minute, expected: "PT1H30M"},
		{name: "hours only", input: 2 * time.Hour, expected: "PT2H"},
		{name: "seconds only", input: 45 * time.Second, expected: "PT45S"},
		{name: "hours minutes and seconds", input: 1*time.Hour + 30*time.Minute + 15*time.Second, expected: "PT1H30M15S"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, DurationToISO8601(tt.input))
		})
	}
}

func TestParseISO8601Duration(t *testing.T) {
	t.Run("empty string returns zero duration without error", func(t *testing.T) {
		d, err := ParseISO8601Duration("")
		assert.NoError(t, err)
		assert.Equal(t, time.Duration(0), d)
	})

	t.Run("minutes only", func(t *testing.T) {
		d, err := ParseISO8601Duration("PT30M")
		assert.NoError(t, err)
		assert.Equal(t, 30*time.Minute, d)
	})

	t.Run("hours and minutes", func(t *testing.T) {
		d, err := ParseISO8601Duration("PT1H30M")
		assert.NoError(t, err)
		assert.Equal(t, 90*time.Minute, d)
	})

	t.Run("hours minutes and seconds", func(t *testing.T) {
		d, err := ParseISO8601Duration("PT1H30M15S")
		assert.NoError(t, err)
		assert.Equal(t, 1*time.Hour+30*time.Minute+15*time.Second, d)
	})

	t.Run("invalid string returns error", func(t *testing.T) {
		_, err := ParseISO8601Duration("not-a-period")
		assert.Error(t, err)
	})
}

func TestDurationISO8601RoundTrip(t *testing.T) {
	durations := []time.Duration{
		30 * time.Minute,
		90 * time.Minute,
		2 * time.Hour,
		45 * time.Second,
		1*time.Hour + 30*time.Minute + 15*time.Second,
	}

	for _, d := range durations {
		iso := DurationToISO8601(d)
		parsed, err := ParseISO8601Duration(iso)
		assert.NoError(t, err)
		assert.Equal(t, d, parsed, "round-trip failed for %v (via %q)", d, iso)
	}
}
