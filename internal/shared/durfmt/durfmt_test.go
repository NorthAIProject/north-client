package durfmt_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/durfmt"
)

func TestHoursMinutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		minutes int
		want    string
	}{
		{0, "0h 00m"},
		{45, "0h 45m"},
		{60, "1h 00m"},
		{61, "1h 01m"},
		{125, "2h 05m"},
		{600, "10h 00m"},
		{-61, "-1h -1m"},
	}

	for _, tt := range tests {
		if got := durfmt.HoursMinutes(tt.minutes); got != tt.want {
			t.Errorf("HoursMinutes(%d) = %q, want %q", tt.minutes, got, tt.want)
		}
	}
}
