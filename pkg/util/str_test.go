package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetTripKey(t *testing.T) {
	assert.Equal(t, "passenger_1OUTBOUND", GetTripKey("passenger_1", "OUTBOUND"))
}

func TestDurationToString(t *testing.T) {
	assert.Equal(t, "01:00", DurationToString(time.Hour))
	assert.Equal(t, "09:00", DurationToString(time.Hour*9))
	assert.Equal(t, "10:00", DurationToString(time.Hour*10))
	assert.Equal(t, "09:59", DurationToString(time.Minute*599))
	assert.Equal(t, "00:01", DurationToString(time.Minute))
	assert.Equal(t, "00:59", DurationToString(time.Minute*59))
	assert.Equal(t, "01:01", DurationToString(time.Minute*61))
	assert.Equal(t, "01:09", DurationToString(time.Minute*69))
	assert.Equal(t, "01:10", DurationToString(time.Minute*70))
	assert.Equal(t, "23:59", DurationToString(time.Minute*1439))
}

func TestToUpper(t *testing.T) {
	assert.Equal(t, []string{"TO", "UPPER", "STRINGS"}, ToUpper([]string{"to", "UppEr", "STRINGS"}))
}

func TestMatchesWildCard(t *testing.T) {
	type scenario struct {
		name     string
		pattern  string
		input    string
		expected bool
	}
	scenarios := []*scenario{
		{
			name:     "exact match",
			pattern:  "service",
			input:    "service",
			expected: true,
		},
		{
			name:     "case insensitive match",
			pattern:  "SERVICE",
			input:    "service",
			expected: true,
		},
		{
			name:     "length mismatch",
			pattern:  "servic",
			input:    "service",
			expected: false,
		},
		{
			name:     "wildcard match",
			pattern:  "ser?ice",
			input:    "service",
			expected: true,
		},
		{
			name:     "wildcard case-insensitive match",
			pattern:  "ser?ice",
			input:    "SERVICE",
			expected: true,
		},
		{
			name:     "wildcard length mismatch",
			pattern:  "servi?",
			input:    "service",
			expected: false,
		},
		{
			name:     "all wildcards match",
			pattern:  "???",
			input:    "xyz",
			expected: true,
		},
		{
			name:     "all wildcards length mismatch",
			pattern:  "???",
			input:    "xy",
			expected: false,
		},
		{
			name:     "both blank match",
			pattern:  "",
			input:    "",
			expected: true,
		},
		{
			name:     "wildcard blank mismatch",
			pattern:  "?",
			input:    "",
			expected: false,
		},
		{
			name:     "wildcard literal ? match",
			pattern:  "?",
			input:    "?",
			expected: true,
		},
		{
			name:     "single wildcard match",
			pattern:  "?",
			input:    "a",
			expected: true,
		},
		{
			name:     "emoji rune exact match",
			pattern:  "😊",
			input:    "😊",
			expected: true,
		},
		{
			name:     "emoji rune wildcard match",
			pattern:  "a?c",
			input:    "a😊c",
			expected: true,
		},
		{
			name:     "ç rune wildcard mismatch",
			pattern:  "a?ç",
			input:    "abc",
			expected: false,
		},
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			actual := MatchesWildCard(s.pattern, s.input)
			assert.Equal(t, s.expected, actual)
		})
	}
}
