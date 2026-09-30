package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWholeDaysBetween(t *testing.T) {
	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want int
	}{
		{
			name: "exactly one day",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "partial day does not count",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 1, 1, 23, 59, 59, 0, time.UTC),
			want: 0,
		},
		{
			name: "one day plus a fraction floors down",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 1, 2, 18, 0, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "hundred days",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
			want: 100,
		},
		{
			name: "full non-leap year",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 365,
		},
		{
			name: "full leap year",
			from: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 366,
		},
		{
			name: "identical instants",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 0,
		},
		{
			name: "reversed order clamps to zero",
			from: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 0,
		},
		{
			name: "across a DST boundary in a non-UTC zone still counts absolute days",
			from: time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, WholeDaysBetween(tt.from, tt.to))
		})
	}
}

func TestWholeMonthsBetween(t *testing.T) {
	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want int
	}{
		{
			name: "exactly one month",
			from: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "one day short of a month",
			from: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC),
			want: 0,
		},
		{
			name: "twelve months",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 12,
		},
		{
			name: "eleven months and most of a twelfth",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			want: 11,
		},
		{
			name: "jan 31 to feb 28 in a non-leap year counts a whole month",
			from: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "jan 31 to feb 29 in a leap year counts a whole month",
			from: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "jan 31 to mar 1 counts one month not two",
			from: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "time of day is respected",
			from: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 2, 15, 11, 0, 0, 0, time.UTC),
			want: 0,
		},
		{
			name: "identical instants",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 0,
		},
		{
			name: "reversed order clamps to zero",
			from: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 0,
		},
		// Mixed zones. The month and year are read off the calendar, so without normalisation
		// these disagree with the same instants expressed in UTC. The westward cases are the
		// dangerous ones: they used to come out a whole month low, which under-reports
		// consumption and over-refunds.
		{
			name: "westward offset just past the anniversary still counts the month",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			// Local calendar says 31 Jan, but the instant is 1 Feb 04:00 UTC.
			to:   time.Date(2026, 1, 31, 23, 0, 0, 0, time.FixedZone("west", -5*60*60)),
			want: 1,
		},
		{
			name: "westward offset across several months",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			// Local calendar says 31 Mar, but the instant is 1 Apr 04:00 UTC.
			to:   time.Date(2026, 3, 31, 23, 0, 0, 0, time.FixedZone("west", -5*60*60)),
			want: 3,
		},
		{
			name: "eastward offset just short of the anniversary counts nothing",
			from: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			// Local calendar says 1 Feb, but the instant is 31 Jan 22:30 UTC.
			to:   time.Date(2026, 2, 1, 0, 30, 0, 0, time.FixedZone("east", 2*60*60)),
			want: 0,
		},
		{
			name: "both sides non-UTC agree with the same instants in UTC",
			from: time.Date(2026, 1, 1, 2, 0, 0, 0, time.FixedZone("east", 2*60*60)),    // 1 Jan 00:00 UTC
			to:   time.Date(2026, 3, 31, 19, 0, 0, 0, time.FixedZone("west", -5*60*60)), // 1 Apr 00:00 UTC
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, WholeMonthsBetween(tt.from, tt.to))

			// The result must depend only on the instants, never on how they are expressed.
			assert.Equal(t, tt.want, WholeMonthsBetween(tt.from.UTC(), tt.to.UTC()),
				"same instants in UTC must give the same answer")
		})
	}
}
