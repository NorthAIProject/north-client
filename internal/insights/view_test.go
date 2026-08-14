package insights

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func weekRange(t *testing.T) timerange.Range {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return timerange.Parse(timerange.KeyWeek, loc)
}

// Chips must be ordered and complete, and the "Everything" chip must count the
// whole feed rather than the filtered slice — otherwise selecting a filter
// makes the total appear to shrink.
func TestTimelineViewChips(t *testing.T) {
	rg := weekRange(t)
	view := buildTimelineView(TimelineData{
		Range: rg,
		Kind:  string(dashboard.KindSleep),
		Total: 9,
		Counts: map[dashboard.EntryKind]int{
			dashboard.KindCheckIn: 4,
			dashboard.KindSleep:   3,
			dashboard.KindHabit:   2,
		},
	})

	if len(view.Chips) != 4 {
		t.Fatalf("chips = %d, want 4 (everything + three kinds)", len(view.Chips))
	}
	if view.Chips[0].Key != "" || view.Chips[0].Count != 9 {
		t.Errorf("first chip = %+v, want the everything chip counting 9", view.Chips[0])
	}
	if view.Chips[0].Selected {
		t.Error("everything must not be selected while a kind filter is active")
	}

	var sleepChip insightpages.FilterChip
	for _, c := range view.Chips {
		if c.Key == string(dashboard.KindSleep) {
			sleepChip = c
		}
	}
	if !sleepChip.Selected {
		t.Error("the active kind's chip must be selected")
	}
	if sleepChip.Count != 3 {
		t.Errorf("sleep chip count = %d, want 3", sleepChip.Count)
	}

	// Kinds with nothing in the window are omitted rather than shown as zero.
	for _, c := range view.Chips {
		if c.Key != "" && c.Count == 0 {
			t.Errorf("chip %q has no entries and should not be rendered", c.Key)
		}
	}
}

// Every bucket in the window gets a column, including the days with no rows.
// Dropping empty days would shift every label after the gap.
func TestBodyViewFillsEveryBucket(t *testing.T) {
	rg := weekRange(t)
	buckets := rg.Buckets()

	// Two days of data inside a seven-day window.
	day0 := buckets[0].Start
	day3 := buckets[3].Start

	view, err := buildBodyView(BodyData{
		Range: rg,
		Hydration: []hydration.Day{
			{Date: day0, TotalML: 500},
			{Date: day3, TotalML: 1500},
		},
		Nights: []sleep.Log{
			{LocalDate: day0, DurationMinutes: 420},
			{LocalDate: day3, DurationMinutes: 480},
		},
		SleepTrend: sleep.Trend{AverageMinutes: 450, AverageQuality: 4, QualityCount: 2, Nights: 2},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := len(view.WaterChart.Data.Datasets[0].Data); got != len(buckets) {
		t.Errorf("water series = %d points, want %d", got, len(buckets))
	}
	if got := len(view.SleepChart.Data.Datasets[0].Data); got != len(buckets) {
		t.Errorf("sleep series = %d points, want %d", got, len(buckets))
	}
	if view.TotalWaterML != 2000 {
		t.Errorf("total water = %d, want 2000", view.TotalWaterML)
	}
	if !view.HasWater || !view.HasSleep {
		t.Error("expected both series to report data")
	}
	if view.QualityCount != 2 {
		t.Errorf("QualityCount = %d, want 2", view.QualityCount)
	}
}

func TestBodyViewEmptyWindowReportsNoData(t *testing.T) {
	view, err := buildBodyView(BodyData{Range: weekRange(t)})
	if err != nil {
		t.Fatal(err)
	}
	if view.HasWater {
		t.Error("an empty window has no water data")
	}
	if view.HasSleep {
		t.Error("an empty window has no sleep data")
	}
	if view.HasHabits {
		t.Error("an empty window has no habits")
	}
	if view.TotalWaterML != 0 {
		t.Errorf("total water = %d, want 0", view.TotalWaterML)
	}
}

// The averages must be over days recorded, not days elapsed. Someone who
// checked in twice at mood 4 has an average of 4, not 8/7.
func TestMindViewAveragesOverDaysRecorded(t *testing.T) {
	rg := weekRange(t)
	buckets := rg.Buckets()

	view, err := buildMindView(MindData{
		Range: rg,
		CheckIns: []checkins.CheckIn{
			{LocalDate: buckets[0].Start, Mood: 4, Energy: 2},
			{LocalDate: buckets[2].Start, Mood: 4, Energy: 4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if view.CheckInCount != 2 {
		t.Fatalf("CheckInCount = %d, want 2", view.CheckInCount)
	}
	if view.AvgMood != 4 {
		t.Errorf("AvgMood = %v, want 4", view.AvgMood)
	}
	if view.AvgEnergy != 3 {
		t.Errorf("AvgEnergy = %v, want 3", view.AvgEnergy)
	}
	if !view.HasCheckIns {
		t.Error("expected check-in data")
	}
	if view.HasJournal {
		t.Error("no journal entries were supplied")
	}
}

func TestDeltaViewWithoutPriorMakesNoClaim(t *testing.T) {
	d := deltaView(500, 0)
	if d.HasPrior {
		t.Fatal("a zero prior window is not something to compare against")
	}
	if d.Pct != 0 || d.Direction != 0 {
		t.Fatalf("expected an empty delta, got %+v", d)
	}
}

func TestDeltaViewDirections(t *testing.T) {
	up := deltaView(150, 100)
	if !up.HasPrior || up.Direction != 1 || up.Pct != 50 {
		t.Errorf("up = %+v, want +50%% rising", up)
	}
	down := deltaView(50, 100)
	if down.Direction != -1 || down.Pct != -50 {
		t.Errorf("down = %+v, want -50%% falling", down)
	}
	flat := deltaView(100, 100)
	if flat.Direction != 0 {
		t.Errorf("flat = %+v, want no direction", flat)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds int
		want    string
	}{
		{0, "0m"},
		{90, "1m"},
		{600, "10m"},
		{3600, "1h"},
		{5400, "1h 30m"},
		{7200, "2h"},
	}
	for _, tc := range tests {
		if got := formatDuration(tc.seconds); got != tc.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
