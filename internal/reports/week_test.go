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
