package viz

import "testing"

func TestSparklineDrawsNoChrome(t *testing.T) {
	// A sparkline sits inside a card beside its own number. Axes, grid lines
	// and a legend would repeat what the card already says and crowd out the
	// shape, which is the only thing the reader is here for.
	got := Sparkline("pinned-water", []string{"Mon", "Tue"}, []float64{1, 2})

	if got.ShowLegend {
		t.Error("ShowLegend = true, want false")
	}
	if got.ShowXLabels || got.ShowYLabels {
		t.Errorf("axis labels shown: x=%v y=%v, want both false", got.ShowXLabels, got.ShowYLabels)
	}
	if got.ShowXGrid || got.ShowYGrid {
		t.Errorf("grid shown: x=%v y=%v, want both false", got.ShowXGrid, got.ShowYGrid)
	}
}

func TestSparklineCarriesItsData(t *testing.T) {
	got := Sparkline("pinned-water", []string{"Mon", "Tue"}, []float64{1, 2})

	if got.ID != "pinned-water" {
		t.Errorf("ID = %q, want %q", got.ID, "pinned-water")
	}
	if len(got.Data.Datasets) != 1 {
		t.Fatalf("datasets = %d, want 1", len(got.Data.Datasets))
	}
	if len(got.Data.Datasets[0].Data) != 2 {
		t.Errorf("points = %d, want 2", len(got.Data.Datasets[0].Data))
	}
}

func TestGroupedBarCarriesEverySeries(t *testing.T) {
	got := GroupedBar("turns", []string{"Mon", "Tue"}, []BarSeries{
		{Label: "You", Values: []float64{1, 2}},
		{Label: "Coach", Values: []float64{3, 4}},
	})

	if len(got.Data.Datasets) != 2 {
		t.Fatalf("datasets = %d, want 2", len(got.Data.Datasets))
	}
	if got.Data.Datasets[0].Label != "You" || got.Data.Datasets[1].Label != "Coach" {
		t.Errorf("labels = %q, %q", got.Data.Datasets[0].Label, got.Data.Datasets[1].Label)
	}
	if !got.ShowLegend {
		t.Error("ShowLegend = false — two series need naming")
	}
}

func TestGroupedBarGivesEachSeriesItsOwnColour(t *testing.T) {
	// Two bars in one colour is one bar with a confusing shape.
	got := GroupedBar("turns", []string{"Mon"}, []BarSeries{
		{Label: "You", Values: []float64{1}},
		{Label: "Coach", Values: []float64{2}},
	})

	if got.Data.Datasets[0].BackgroundColor == got.Data.Datasets[1].BackgroundColor {
		t.Error("both series share a colour")
	}
}
