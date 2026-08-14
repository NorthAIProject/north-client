package timerange

import (
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}

func TestParseUnknownKeyIsToday(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")
	for _, q := range []string{"", "nonsense", "TODAY", "last-decade", "../../etc/passwd"} {
		r := Parse(q, loc)
		if r.Key != KeyToday {
			t.Errorf("Parse(%q).Key = %q, want %q", q, r.Key, KeyToday)
		}
	}
}

func TestParseNilLocationIsUTC(t *testing.T) {
	r := Parse(KeyWeek, nil)
	if r.Location() != time.UTC {
		t.Fatalf("Location() = %v, want UTC", r.Location())
	}
}

func TestParseWindowsAreHalfOpenAndAnchoredToMidnight(t *testing.T) {
	loc := mustLoad(t, "Pacific/Auckland")

	tests := []struct {
		key      string
		wantDays int
	}{
		{KeyToday, 1},
		{KeyYesterday, 1},
		{KeyWeek, 7},
		{KeyMonth, 30},
		{KeyQuarter, 90},
	}

	for _, tc := range tests {
		r := Parse(tc.key, loc)

		if h, m, s := r.Since.Clock(); h != 0 || m != 0 || s != 0 {
			t.Errorf("%s: Since = %v, want midnight", tc.key, r.Since)
		}
		if !r.Since.Before(r.Until) {
			t.Errorf("%s: Since %v not before Until %v", tc.key, r.Since, r.Until)
		}
		if got := r.Days(); got != tc.wantDays {
			t.Errorf("%s: Days() = %d, want %d", tc.key, got, tc.wantDays)
		}
	}
}

func TestContainsIsHalfOpen(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")
	r := Parse(KeyToday, loc)

	if !r.Contains(r.Since) {
		t.Error("Since must be inside the window")
	}
	if r.Contains(r.Until) {
		t.Error("Until must be outside the window")
	}
	if r.Contains(r.Since.Add(-time.Nanosecond)) {
		t.Error("instant before Since must be outside")
	}
	if !r.Contains(r.Until.Add(-time.Nanosecond)) {
		t.Error("last instant before Until must be inside")
	}
}

// Contains must judge by the reader's clock, not the instant's own zone. Two
// identical instants written in different zones are the same moment and belong
// in the same bucket.
func TestContainsNormalisesIncomingZone(t *testing.T) {
	loc := mustLoad(t, "Pacific/Auckland")
	r := Parse(KeyToday, loc)

	noon := r.Since.Add(12 * time.Hour)
	if !r.Contains(noon.UTC()) {
		t.Error("the same instant expressed in UTC must still be contained")
	}
}

func TestPreviousAbutsWithoutOverlap(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")

	for _, key := range []string{KeyToday, KeyYesterday, KeyWeek, KeyMonth, KeyQuarter} {
		r := Parse(key, loc)
		prev := r.Previous()

		if !prev.Until.Equal(r.Since) {
			t.Errorf("%s: Previous().Until = %v, want %v", key, prev.Until, r.Since)
		}
		if prev.Days() != r.Days() {
			t.Errorf("%s: Previous().Days() = %d, want %d", key, prev.Days(), r.Days())
		}
		if prev.Contains(r.Since) {
			t.Errorf("%s: previous window must not contain the current window's start", key)
		}
	}
}

// A window spanning a DST transition is still N calendar days, even though it
// is not N*24 hours. Dividing the duration would report six days for a week.
func TestDaysCountsCalendarDaysAcrossDST(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")

	// Lisbon springs forward on 2026-03-29: that day is 23 hours long.
	since := time.Date(2026, 3, 26, 0, 0, 0, 0, loc)
	until := time.Date(2026, 4, 2, 0, 0, 0, 0, loc)
	r := Range{Key: KeyWeek, Since: since, Until: until, Grain: GrainDay, loc: loc}

	if got := r.Days(); got != 7 {
		t.Fatalf("Days() = %d, want 7", got)
	}
	if hours := until.Sub(since).Hours(); hours != 167 {
		t.Fatalf("precondition: expected a 167-hour week, got %v", hours)
	}
}

func TestDayBucketsTileTheWindowAcrossDST(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")
	since := time.Date(2026, 3, 26, 0, 0, 0, 0, loc)
	until := time.Date(2026, 4, 2, 0, 0, 0, 0, loc)
	r := Range{Key: KeyWeek, Since: since, Until: until, Grain: GrainDay, loc: loc}

	buckets := r.Buckets()
	if len(buckets) != 7 {
		t.Fatalf("buckets = %d, want 7", len(buckets))
	}
	if !buckets[0].Start.Equal(since) {
		t.Errorf("first bucket starts at %v, want %v", buckets[0].Start, since)
	}
	if !buckets[len(buckets)-1].End.Equal(until) {
		t.Errorf("last bucket ends at %v, want %v", buckets[len(buckets)-1].End, until)
	}
	for i := 1; i < len(buckets); i++ {
		if !buckets[i].Start.Equal(buckets[i-1].End) {
			t.Errorf("gap between bucket %d and %d: %v vs %v",
				i-1, i, buckets[i-1].End, buckets[i].Start)
		}
	}
}

func TestHourBucketsCoverASingleDay(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")
	r := Parse(KeyToday, loc)

	buckets := r.Buckets()
	if len(buckets) != 24 {
		t.Fatalf("buckets = %d, want 24", len(buckets))
	}
	if buckets[0].Label != "00:00" {
		t.Errorf("first label = %q, want %q", buckets[0].Label, "00:00")
	}
}

func TestWeekBucketsClampTheFinalBucket(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")
	r := Parse(KeyQuarter, loc)

	buckets := r.Buckets()
	if len(buckets) == 0 {
		t.Fatal("expected week buckets")
	}
	last := buckets[len(buckets)-1]
	if last.End.After(r.Until) {
		t.Errorf("last bucket ends at %v, past the window end %v", last.End, r.Until)
	}
}

func TestIndexFindsTheRightBucketAndRejectsOutsiders(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")
	r := Parse(KeyWeek, loc)
	buckets := r.Buckets()

	for i, b := range buckets {
		mid := b.Start.Add(b.End.Sub(b.Start) / 2)
		if got := r.Index(buckets, mid); got != i {
			t.Errorf("Index(mid of bucket %d) = %d", i, got)
		}
	}
	if got := r.Index(buckets, r.Since.Add(-time.Hour)); got != -1 {
		t.Errorf("Index(before window) = %d, want -1", got)
	}
	if got := r.Index(buckets, r.Until); got != -1 {
		t.Errorf("Index(at Until) = %d, want -1", got)
	}
}

func TestLabelsMatchBuckets(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")
	r := Parse(KeyMonth, loc)

	labels := r.Labels()
	buckets := r.Buckets()
	if len(labels) != len(buckets) {
		t.Fatalf("labels = %d, buckets = %d", len(labels), len(buckets))
	}
	for i := range labels {
		if labels[i] != buckets[i].Label {
			t.Errorf("label %d = %q, want %q", i, labels[i], buckets[i].Label)
		}
	}
}

func TestAllReturnsEverySelectorOption(t *testing.T) {
	loc := mustLoad(t, "Europe/Lisbon")
	all := All(loc)

	want := []string{KeyToday, KeyYesterday, KeyWeek, KeyMonth, KeyQuarter}
	if len(all) != len(want) {
		t.Fatalf("All() = %d ranges, want %d", len(all), len(want))
	}
	for i, r := range all {
		if r.Key != want[i] {
			t.Errorf("All()[%d].Key = %q, want %q", i, r.Key, want[i])
		}
		if r.Label == "" {
			t.Errorf("All()[%d] has no label", i)
		}
	}
}
