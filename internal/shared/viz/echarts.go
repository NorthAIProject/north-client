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

// TrendBand is one stacked band of the training chart: a sport family, its
// colour, and its minutes on each drawn day.
type TrendBand struct {
	Label  string
	Color  string
	Values []float64
}

// TrainingTrendJSON returns the option for the training chart: a stacked bar
// per day, and a line for what a normal day holds.
//
// Built as a plain option map rather than through go-echarts. The chart is a
// stack of a variable number of bar series with a line overlaid on the same
// axes, and expressing that through Overlap costs more code than it saves
// while hiding which keys actually reach ECharts. Everything here is ordinary
// JSON, which is all the mount consumes.
//
// Colours arrive as CSS custom properties. echarts-init.js resolves them
// against the document before handing the option to ECharts, so one option
// serves both themes and follows the theme switch.
func TrainingTrendJSON(labels []string, bands []TrendBand, normalLabel string, normal []float64) ([]byte, error) {
	series := make([]map[string]any, 0, len(bands)+1)

	for _, band := range bands {
		if !anyAbove(band.Values, 0) {
			// A family nobody trains is not drawn, and does not take a slot in
			// the legend either. Six empty bands would make every chart look
			// like it was mostly missing.
			continue
		}
		series = append(series, map[string]any{
			"name":        band.Label,
			"type":        "bar",
			"stack":       "minutes",
			"data":        band.Values,
			"itemStyle":   map[string]any{"color": band.Color, "borderRadius": []int{2, 2, 0, 0}},
			"barMaxWidth": 18,
		})
	}

	series = append(series, map[string]any{
		"name":       normalLabel,
		"type":       "line",
		"data":       normal,
		"smooth":     true,
		"showSymbol": false,
		"lineStyle":  map[string]any{"color": "var(--foreground)", "width": 2, "type": "dashed"},
		"itemStyle":  map[string]any{"color": "var(--foreground)"},
		"z":          3,
	})

	option := map[string]any{
		"grid": map[string]any{"left": 44, "right": 12, "top": 28, "bottom": 28},
		"tooltip": map[string]any{
			"trigger":     "axis",
			"axisPointer": map[string]any{"type": "shadow"},
		},
		"legend": map[string]any{
			"show":       true,
			"bottom":     0,
			"itemWidth":  8,
			"itemHeight": 8,
			"textStyle":  map[string]any{"color": "var(--muted-foreground)", "fontSize": 10},
		},
		"xAxis": map[string]any{
			"type":      "category",
			"data":      labels,
			"axisLine":  map[string]any{"lineStyle": map[string]any{"color": "var(--border)"}},
			"axisTick":  map[string]any{"show": false},
			"axisLabel": map[string]any{"color": "var(--muted-foreground)", "fontSize": 10, "interval": "auto"},
		},
		"yAxis": map[string]any{
			"type":          "value",
			"name":          "minutes",
			"nameTextStyle": map[string]any{"color": "var(--muted-foreground)", "fontSize": 10, "align": "left"},
			"axisLabel":     map[string]any{"color": "var(--muted-foreground)", "fontSize": 10},
			"splitLine":     map[string]any{"lineStyle": map[string]any{"color": "var(--border)", "opacity": 0.4}},
		},
		"series": series,
	}
	return json.Marshal(option)
}

func anyAbove(values []float64, floor float64) bool {
	for _, v := range values {
		if v > floor {
			return true
		}
	}
	return false
}
