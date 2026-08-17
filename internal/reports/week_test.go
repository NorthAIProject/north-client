package reports

import (
	"testing"
	"time"
)

func TestWeekContainingStartsMonday(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	// Thursday 13 Aug 2026, late evening in Lisbon.
	at := time.Date(2026, 8, 13, 22, 0, 0, 0, loc)
	week := WeekContaining(at, loc)

	if week.Start.Weekday() != time.Monday {
		t.Fatalf("start weekday = %s", week.Start.Weekday())
	}
	if got := week.Start.Format("2006-01-02"); got != "2026-08-10" {
		t.Fatalf("start = %s, want 2026-08-10", got)
	}
	if got := week.End.Format("2006-01-02"); got != "2026-08-17" {
		t.Fatalf("end = %s, want 2026-08-17", got)
	}
	if week.Title() != "Week of 10 Aug 2026" {
		t.Fatalf("title = %q", week.Title())
	}
	if !week.Contains(at) {
		t.Fatal("the instant that defined the week is outside it")
	}
	if week.Contains(week.End) {
		t.Fatal("end is exclusive")
	}
}

func TestDayContainingIsOneLocalDay(t *testing.T) {
	loc, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatal(err)
	}
	// Late evening local, which is the previous day in UTC. The briefing must
	// still be about the reader's Monday, not UTC's Sunday.
	at := time.Date(2026, 8, 17, 23, 30, 0, 0, loc)
	day := DayContaining(at, loc)

	if got := day.Start.Format("2006-01-02"); got != "2026-08-17" {
		t.Fatalf("start = %s, want 2026-08-17", got)
	}
	if got := day.End.Format("2006-01-02"); got != "2026-08-18" {
		t.Fatalf("end = %s, want 2026-08-18", got)
	}
	if h, m, s := day.Start.Clock(); h != 0 || m != 0 || s != 0 {
		t.Fatalf("start is not local midnight: %02d:%02d:%02d", h, m, s)
	}
	if day.Title != "Briefing for Monday 17 Aug" {
		t.Fatalf("title = %q", day.Title)
	}
}

// A nil location must not panic; UTC is the documented fallback.
func TestDayContainingDefaultsToUTC(t *testing.T) {
	day := DayContaining(time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC), nil)
	if day.Start.Location() != time.UTC {
		t.Fatalf("location = %s, want UTC", day.Start.Location())
	}
}
