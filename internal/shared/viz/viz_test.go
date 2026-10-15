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

// The gauge draws nothing unless its track colour is a list of stops; a bare
// string leaves ECharts with no arc. Its legend and own title stay hidden,
// because the card prints the heading and the title would sit on the percent.
func TestGaugeDrawsTrackWithoutLegendOrTitle(t *testing.T) {
	raw, err := viz.GaugeOptionJSON("Body", 87)
	if err != nil {
		t.Fatal(err)
	}
	var opt struct {
		Legend struct {
			Show bool `json:"show"`
		} `json:"legend"`
		Series []struct {
			AxisLine struct {
				LineStyle struct {
					Color [][]any `json:"color"`
				} `json:"lineStyle"`
			} `json:"axisLine"`
			Progress struct {
				Show      bool `json:"show"`
				ItemStyle struct {
					Color string `json:"color"`
				} `json:"itemStyle"`
			} `json:"progress"`
			Title struct {
				Show bool `json:"show"`
			} `json:"title"`
		} `json:"series"`
	}
	if err := json.Unmarshal(raw, &opt); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	if opt.Legend.Show {
		t.Error("legend shown; want hidden")
	}
	if len(opt.Series) != 1 {
		t.Fatalf("series = %d want 1", len(opt.Series))
	}
	s := opt.Series[0]
	stops := s.AxisLine.LineStyle.Color
	if len(stops) != 1 || len(stops[0]) != 2 || stops[0][1] != "var(--border)" {
		t.Errorf("axis line colour = %#v; want [[1, var(--border)]]", stops)
	}
	if !s.Progress.Show || s.Progress.ItemStyle.Color != "var(--north-signal)" {
		t.Errorf("progress = %+v; want shown in var(--north-signal)", s.Progress)
	}
	if s.Title.Show {
		t.Error("gauge title shown; it overlaps the percent")
	}
}

// The donut's legend and slices take theme tokens, never ECharts' defaults.
func TestDonutUsesThemeTokens(t *testing.T) {
	raw, err := viz.DonutOptionJSON([]viz.DonutSegment{{Label: "A", Value: 1}})
	if err != nil {
		t.Fatal(err)
	}
	var opt struct {
		Color  []string `json:"color"`
		Legend struct {
			TextStyle struct {
				Color string `json:"color"`
			} `json:"textStyle"`
		} `json:"legend"`
	}
	if err := json.Unmarshal(raw, &opt); err != nil {
		t.Fatal(err)
	}
	if opt.Legend.TextStyle.Color != "var(--muted-foreground)" {
		t.Errorf("legend text colour = %q", opt.Legend.TextStyle.Color)
	}
	if len(opt.Color) == 0 {
		t.Fatal("no slice palette; ECharts would fall back to its own")
	}
	for _, c := range opt.Color {
		if len(c) < 6 || c[:6] != "var(--" {
			t.Errorf("palette colour %q is not a theme token", c)
		}
	}
}
