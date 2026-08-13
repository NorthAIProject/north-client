package viz

import (
	"encoding/json"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
)

// DonutSegment is one slice of a category donut.
type DonutSegment struct {
	Label string
	Value int
}

// GaugeOptionJSON returns an ECharts gauge option for a 0–100 percent.
func GaugeOptionJSON(title string, percent int) ([]byte, error) {
	percent = clampPercent(percent)
	g := charts.NewGauge()
	g.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{Title: title, Show: opts.Bool(false)}),
	)
	g.AddSeries("value", []opts.GaugeData{
		{Value: float64(percent), Name: title},
	}, charts.WithSeriesOpts(func(s *charts.SingleSeries) {
		s.Min = 0
		s.Max = 100
		s.StartAngle = 210
		s.EndAngle = -30
		s.Progress = &opts.Progress{Show: opts.Bool(true), Width: 10}
		s.AxisLine = &opts.AxisLine{
			LineStyle: &opts.LineStyle{Width: 10, Color: "var(--border)"},
		}
		s.AxisTick = &opts.AxisTick{Show: opts.Bool(false)}
		s.SplitLine = &opts.SplitLine{Show: opts.Bool(false)}
		s.AxisLabel = &opts.AxisLabel{Show: opts.Bool(false)}
		s.Pointer = &opts.Pointer{Show: opts.Bool(false)}
		s.Detail = &opts.Detail{
			Formatter: "{value}%",
			Color:     "var(--foreground)",
			FontSize:  18,
		}
	}))
	return json.Marshal(g.JSON())
}

// HeatmapCell is one day in a mood heatmap.
type HeatmapCell struct {
	Label string
	Value int // 1–5; zero omitted
}

// HeatmapJSON returns an ECharts heatmap option.
func HeatmapJSON(rowLabel string, cells []HeatmapCell) ([]byte, error) {
	labels := make([]string, 0, len(cells))
	data := make([]opts.HeatMapData, 0, len(cells))
	for _, c := range cells {
		if c.Value <= 0 {
			continue
		}
		idx := len(labels)
		labels = append(labels, c.Label)
		data = append(data, opts.HeatMapData{Value: []any{idx, 0, c.Value}})
	}

	hm := charts.NewHeatMap()
	hm.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{Show: opts.Bool(false)}),
		charts.WithXAxisOpts(opts.XAxis{
			Type:      "category",
			Data:      labels,
			SplitArea: &opts.SplitArea{Show: opts.Bool(true)},
			AxisLabel: &opts.AxisLabel{Show: opts.Bool(true), Color: "var(--muted-foreground)"},
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Type:      "category",
			Data:      []string{rowLabel},
			SplitArea: &opts.SplitArea{Show: opts.Bool(true)},
			AxisLabel: &opts.AxisLabel{Show: opts.Bool(false)},
		}),
		charts.WithVisualMapOpts(opts.VisualMap{
			Min:        1,
			Max:        5,
			Calculable: opts.Bool(false),
			Orient:     "horizontal",
			Left:       "center",
			Bottom:     "0%",
			InRange: &opts.VisualMapInRange{
				Color: []string{
					"color-mix(in oklch, var(--north-signal) 20%, var(--background))",
					"var(--north-signal)",
				},
			},
			TextStyle: &opts.TextStyle{Color: "var(--muted-foreground)"},
		}),
	)
	hm.AddSeries("value", data,
		charts.WithLabelOpts(opts.Label{Show: opts.Bool(false)}),
		charts.WithEmphasisOpts(opts.Emphasis{
			ItemStyle: &opts.ItemStyle{BorderColor: "var(--foreground)"},
		}),
	)
	return json.Marshal(hm.JSON())
}

// DonutOptionJSON returns an ECharts donut for category splits.
func DonutOptionJSON(segments []DonutSegment) ([]byte, error) {
	data := make([]opts.PieData, 0, len(segments))
	for _, s := range segments {
		if s.Value <= 0 {
			continue
		}
		data = append(data, opts.PieData{Name: s.Label, Value: s.Value})
	}
	pie := charts.NewPie()
	pie.SetGlobalOptions(charts.WithTitleOpts(opts.Title{Show: opts.Bool(false)}))
	pie.AddSeries("split", data,
		charts.WithSeriesOpts(func(s *charts.SingleSeries) {
			s.Radius = []string{"55%", "75%"}
			s.Center = []string{"50%", "50%"}
		}),
		charts.WithLabelOpts(opts.Label{Show: opts.Bool(true), Color: "var(--foreground)"}),
	)
	return json.Marshal(pie.JSON())
}

// UnmarshalOption decodes ECharts JSON into a map for templ.JSONScript.
func UnmarshalOption(raw []byte) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func clampPercent(v int) int {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}
