package viz_test

import (
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/viz"
)

func TestClampPercent(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{-5, 0},
		{0, 0},
		{42, 42},
		{100, 100},
		{150, 100},
	}
	for _, tc := range tests {
		raw, err := viz.GaugeOptionJSON("test", tc.in)
		if err != nil {
			t.Fatal(err)
		}
		var opt map[string]any
		if err := json.Unmarshal(raw, &opt); err != nil {
			t.Fatal(err)
		}
		series, ok := opt["series"].([]any)
		if !ok || len(series) == 0 {
			t.Fatalf("series = %#v", opt["series"])
		}
		first, ok := series[0].(map[string]any)
		if !ok {
			t.Fatalf("first series = %#v", series[0])
		}
		data, ok := first["data"].([]any)
		if !ok || len(data) == 0 {
			t.Fatalf("data = %#v", first["data"])
		}
		point, ok := data[0].(map[string]any)
		if !ok {
			t.Fatalf("point = %#v", data[0])
		}
		got, ok := point["value"].(float64)
		if !ok {
			t.Fatalf("value = %#v", point["value"])
		}
		if int(got) != tc.want {
			t.Fatalf("clamp(%d) = %d want %d", tc.in, int(got), tc.want)
		}
	}
}

func TestHeatmapCellCount(t *testing.T) {
	cells := []viz.HeatmapCell{
		{Label: "1 Aug", Value: 4},
		{Label: "2 Aug", Value: 0},
		{Label: "3 Aug", Value: 3},
	}
	raw, err := viz.HeatmapJSON("Mood", cells)
	if err != nil {
		t.Fatal(err)
	}
	var opt map[string]any
	if err := json.Unmarshal(raw, &opt); err != nil {
		t.Fatal(err)
	}
	series := opt["series"].([]any)
	first := series[0].(map[string]any)
	data := first["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("heatmap cells = %d want 2", len(data))
	}
}

func TestDonutSegmentCount(t *testing.T) {
	raw, err := viz.DonutOptionJSON([]viz.DonutSegment{
		{Label: "A", Value: 3},
		{Label: "B", Value: 0},
		{Label: "C", Value: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	var opt map[string]any
	if err := json.Unmarshal(raw, &opt); err != nil {
		t.Fatal(err)
	}
	series := opt["series"].([]any)
	first := series[0].(map[string]any)
	data := first["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("donut segments = %d want 2", len(data))
	}
}
