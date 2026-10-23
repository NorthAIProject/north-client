package firstweek_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/firstweek"
)

func lisbon(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// Europe leaves summer time on 2026-10-25, the week this ships. That Sunday
// is 25 hours long; a day number computed from elapsed hours would call the
// last hour of it the next day, and a window of 24 hours would drop it.
func TestTheDayNumberSurvivesTheClocksGoingBack(t *testing.T) {
	loc := lisbon(t)
	p := firstweek.Programme{
		Timezone:  "Europe/Lisbon",
		StartedOn: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC),
	}

	cases := []struct {
		at   time.Time
		want int
	}{
		{time.Date(2026, 10, 24, 23, 59, 0, 0, loc), 0},
		{time.Date(2026, 10, 25, 0, 30, 0, 0, loc), 1},
		{time.Date(2026, 10, 25, 23, 30, 0, 0, loc), 1}, // 24.5 h after midnight: still Sunday
		{time.Date(2026, 10, 26, 0, 10, 0, 0, loc), 2},
		{time.Date(2026, 10, 31, 12, 0, 0, 0, loc), 7},
		{time.Date(2026, 11, 1, 9, 0, 0, 0, loc), 8},
	}
	for _, c := range cases {
		if got := p.Day(c.at); got != c.want {
			t.Errorf("Day(%s) = %d, want %d", c.at.Format(time.RFC3339), got, c.want)
		}
	}

	rg := p.DayRange(1)
	if got := rg.Until.Sub(rg.Since); got != 25*time.Hour {
		t.Errorf("day 1 lasts %s, want 25h — the window lost the repeated hour", got)
	}
}

// The day follows the week's own zone, not wherever the server or the
// person's phone is now: 23:30 UTC is already tomorrow in Lisbon's summer
// and still today in New York.
func TestTheDayIsCountedInTheWeeksOwnZone(t *testing.T) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 6, 2, 23, 30, 0, 0, time.UTC)

	inLisbon := firstweek.Programme{Timezone: "Europe/Lisbon", StartedOn: start}
	inNewYork := firstweek.Programme{Timezone: "America/New_York", StartedOn: start}
	if got := inLisbon.Day(at); got != 2 {
		t.Errorf("Lisbon day = %d, want 2", got)
	}
	if got := inNewYork.Day(at); got != 1 {
		t.Errorf("New York day = %d, want 1", got)
	}
}

func TestOnlyKnownTemplatesAreAccepted(t *testing.T) {
	for _, ok := range []string{"sleep", " train ", "protein", "journal", "custom"} {
		if _, got := firstweek.NormalizeTemplate(ok); !got {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "Sleep", "meditate"} {
		if _, got := firstweek.NormalizeTemplate(bad); got {
			t.Errorf("%q accepted", bad)
		}
	}
}
