package insights

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

func TestBucketedPlacesADayInsideItsWeek(t *testing.T) {
	// The bug this replaces: buckets were matched by formatting their start
	// as a date, so at week grain only data landing exactly on a week's first
	// day was ever found and the other six days were silently dropped.
	rg := timerange.Parse(timerange.KeyQuarter, time.UTC)
	if rg.Grain != timerange.GrainWeek {
		t.Fatalf("quarter grain = %v, want GrainWeek", rg.Grain)
	}

	midWeek := rg.Since.AddDate(0, 0, 3)
	got := bucketed(rg, []point{{At: midWeek, Value: 1500}})

	if got[0] != 1500 {
		t.Errorf("bucket 0 = %v, want 1500 — a Thursday belongs to its week", got[0])
	}
	for i, v := range got[1:] {
		if v != 0 {
			t.Errorf("bucket %d = %v, want 0", i+1, v)
		}
	}
}

func TestBucketedDoesNotSmearADayAcrossEveryHour(t *testing.T) {
	// The same bug at hour grain: all 24 buckets formatted to one date, so a
	// day's total appeared in every hour — a chart saying somebody drank two
	// litres of water twenty-four times.
	rg := timerange.Parse(timerange.KeyToday, time.UTC)
	if rg.Grain != timerange.GrainHour {
		t.Fatalf("today grain = %v, want GrainHour", rg.Grain)
	}

	got := bucketed(rg, []point{{At: rg.Since, Value: 2000}})

	var seen int
	for _, v := range got {
		if v != 0 {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("%d of %d buckets carry the day's total, want exactly 1", seen, len(got))
	}
}

func TestBucketedSumsPointsSharingABucket(t *testing.T) {
	rg := timerange.Parse(timerange.KeyWeek, time.UTC)
	day := rg.Since.Add(6 * time.Hour)

	got := bucketed(rg, []point{{At: day, Value: 250}, {At: day.Add(time.Hour), Value: 500}})

	if got[0] != 750 {
		t.Errorf("bucket 0 = %v, want 750 — two drinks on one day are one day's water", got[0])
	}
}

func TestBucketedIgnoresPointsOutsideTheWindow(t *testing.T) {
	rg := timerange.Parse(timerange.KeyWeek, time.UTC)

	got := bucketed(rg, []point{
		{At: rg.Since.AddDate(0, 0, -1), Value: 999},
		{At: rg.Until.AddDate(0, 0, 1), Value: 999},
	})

	for i, v := range got {
		if v != 0 {
			t.Errorf("bucket %d = %v, want 0 — the point falls outside the window", i, v)
		}
	}
}

func TestBucketedMeanAveragesTheDaysItCovers(t *testing.T) {
	// A per-day measurement must not be summed into a coarser bucket. Seven
	// eight-hour nights are an eight-hour average, not a fifty-six hour one,
	// and the axis has to mean the same thing at every grain.
	rg := timerange.Parse(timerange.KeyQuarter, time.UTC)

	var week []point
	for d := 0; d < 7; d++ {
		week = append(week, point{At: rg.Since.AddDate(0, 0, d), Value: 8})
	}

	got := bucketedMean(rg, week)

	if got[0] != 8 {
		t.Errorf("bucket 0 = %v, want 8", got[0])
	}
}

func TestBucketedMeanIgnoresDaysWithNoReading(t *testing.T) {
	// Averaging over the bucket's whole width would report a night somebody
	// did not log as a night of no sleep.
	rg := timerange.Parse(timerange.KeyQuarter, time.UTC)

	got := bucketedMean(rg, []point{
		{At: rg.Since, Value: 8},
		{At: rg.Since.AddDate(0, 0, 1), Value: 6},
	})

	if got[0] != 7 {
		t.Errorf("bucket 0 = %v, want 7 — the mean of the two nights logged", got[0])
	}
}

func TestBucketedMeanIsZeroForAnEmptyBucket(t *testing.T) {
	rg := timerange.Parse(timerange.KeyWeek, time.UTC)

	got := bucketedMean(rg, nil)

	for i, v := range got {
		if v != 0 {
			t.Errorf("bucket %d = %v, want 0", i, v)
		}
	}
}
