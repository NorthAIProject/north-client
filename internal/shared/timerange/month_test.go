package timerange

import (
	"testing"
	"time"
)

func lisbon(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

func TestStartOfMonthIsTheFirstInstantOfTheFirst(t *testing.T) {
	loc := lisbon(t)
	got := StartOfMonth(time.Date(2026, 3, 17, 14, 30, 0, 0, loc))

	if got.Day() != 1 || got.Month() != time.March || got.Year() != 2026 {
		t.Errorf("StartOfMonth = %s, want 1 March 2026", got)
	}
	if got.Hour() != 0 {
		t.Errorf("StartOfMonth hour = %d, want 0", got.Hour())
	}
}

func TestStartOfMonthKeepsTheMonthAcrossASpringForward(t *testing.T) {
	// Lisbon springs forward on the last Sunday of March. A month whose first
	// day sits near a transition must not roll into February.
	loc := lisbon(t)
	got := StartOfMonth(time.Date(2026, 3, 29, 3, 30, 0, 0, loc))

	if got.Month() != time.March || got.Day() != 1 {
		t.Errorf("StartOfMonth = %s, want 1 March", got)
	}
}

func TestYearRangeIsTwelveCalendarMonths(t *testing.T) {
	rg := Parse(KeyYear, lisbon(t))

	if rg.Key != KeyYear {
		t.Fatalf("Key = %q, want %q", rg.Key, KeyYear)
	}
	if rg.Grain != GrainMonth {
		t.Fatalf("Grain = %v, want GrainMonth", rg.Grain)
	}

	buckets := rg.Buckets()
	if len(buckets) != 12 {
		t.Fatalf("buckets = %d, want 12", len(buckets))
	}
	if buckets[0].Start.Day() != 1 {
		t.Errorf("first bucket starts on day %d, want 1", buckets[0].Start.Day())
	}
}

func TestMonthBucketsTileWithoutOverlapOrGap(t *testing.T) {
	// Half-open buckets must hand straight over: every bucket's end is the
	// next one's start, or Index can drop a day between them.
	buckets := Parse(KeyYear, lisbon(t)).Buckets()

	for i := 1; i < len(buckets); i++ {
		if !buckets[i].Start.Equal(buckets[i-1].End) {
			t.Errorf("bucket %d starts %s but %d ended %s",
				i, buckets[i].Start, i-1, buckets[i-1].End)
		}
	}
}

func TestMonthBucketsAreNamedByTheirMonth(t *testing.T) {
	buckets := Parse(KeyYear, lisbon(t)).Buckets()

	seen := map[string]bool{}
	for _, b := range buckets {
		if b.Label == "" {
			t.Fatal("a month bucket has no label")
		}
		if seen[b.Label] {
			t.Errorf("two buckets are both labelled %q", b.Label)
		}
		seen[b.Label] = true
	}
}

func TestIndexFindsADayInsideItsMonth(t *testing.T) {
	loc := lisbon(t)
	rg := Parse(KeyYear, loc)
	buckets := rg.Buckets()

	// A day in the middle of the final bucket belongs to it, not to bucket 0.
	last := buckets[len(buckets)-1]
	mid := last.Start.AddDate(0, 0, 1)
	if !mid.Before(last.End) {
		mid = last.Start
	}

	if got := rg.Index(buckets, mid); got != len(buckets)-1 {
		t.Errorf("Index = %d, want %d", got, len(buckets)-1)
	}
}

func TestYearIsOfferedBySelector(t *testing.T) {
	var found bool
	for _, r := range All(lisbon(t)) {
		if r.Key == KeyYear {
			found = true
		}
	}
	if !found {
		t.Error("the selector does not offer a year")
	}
}

func TestYearHasAPriorWindowToCompareWith(t *testing.T) {
	rg := Parse(KeyYear, lisbon(t))
	prev := rg.Previous()

	if !prev.Until.Equal(rg.Since) {
		t.Errorf("previous window ends %s, want %s", prev.Until, rg.Since)
	}
	if !prev.Since.Before(prev.Until) {
		t.Errorf("previous window is empty: %s to %s", prev.Since, prev.Until)
	}
}
