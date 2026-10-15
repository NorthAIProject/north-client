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

// GaugeOptionJSON returns an ECharts gauge option for a 0–100 percent: a
// signal-coloured arc over a hairline track, with the percent in the middle.
//
// Built as a plain option map, like TrainingTrendJSON. go-echarts types the
// axis line colour as a string, and a gauge needs it as stops — [[1, colour]]
// — or ECharts draws no track and no progress arc at all. The library also
// emits an empty legend, which ECharts fills with the series name.
//
// The title is the series' data name for tooltips and screen readers only.
// Every card that mounts a gauge already prints its own heading, and the
// gauge's own title sits on top of the percent.
func GaugeOptionJSON(title string, percent int) ([]byte, error) {
	percent = clampPercent(percent)
	option := map[string]any{
		"legend":  map[string]any{"show": false},
		"tooltip": map[string]any{"show": false},
		"series": []map[string]any{{
			"type":       "gauge",
			"name":       title,
			"min":        0,
			"max":        100,
			"startAngle": 210,
			"endAngle":   -30,
			"radius":     "90%",
			"center":     []string{"50%", "55%"},
			"progress": map[string]any{
				"show":      true,
				"width":     10,
				"roundCap":  true,
				"itemStyle": map[string]any{"color": "var(--north-signal)"},
			},
			"axisLine": map[string]any{
				"roundCap": true,
				"lineStyle": map[string]any{
					"width": 10,
					"color": [][]any{{1, "var(--border)"}},
				},
			},
			"axisTick":  map[string]any{"show": false},
			"splitLine": map[string]any{"show": false},
			"axisLabel": map[string]any{"show": false},
			"pointer":   map[string]any{"show": false},
			"anchor":    map[string]any{"show": false},
			"title":     map[string]any{"show": false},
			"detail": map[string]any{
				"valueAnimation": true,
				"formatter":      "{value}%",
				"color":          "var(--foreground)",
				"fontSize":       20,
				"fontWeight":     600,
				"offsetCenter":   []string{"0", "0"},
			},
			"data": []map[string]any{{"value": percent, "name": title}},
		}},
	}
	return json.Marshal(option)
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

// donutPalette colours the slices of a category donut, in order. North
// tokens rather than the ECharts default palette, whose blue and yellow are
// not North's and which never follows the theme. Signal, agent and ember lead
// because they are the palette's three accents; the sport and tracker
// families fill out the rest. echarts-init.js resolves each var() against the
// live theme.
var donutPalette = []string{
	"var(--north-signal)",
	"var(--north-agent)",
	"var(--north-ember)",
	"var(--north-sport-walk)",
	"var(--north-day-protein)",
	"var(--north-sport-swim)",
	"var(--north-day-move)",
	"var(--north-sport-ride)",
	"var(--north-sport-other)",
}

// DonutOptionJSON returns an ECharts donut for category splits.
//
// A plain option map, like the gauge: the legend and the palette both need
// theme tokens, and go-echarts otherwise emits its own default palette and an
// unstyled legend that ECharts paints #333 — unreadable on the dark card.
func DonutOptionJSON(segments []DonutSegment) ([]byte, error) {
	data := make([]map[string]any, 0, len(segments))
	for _, s := range segments {
		if s.Value <= 0 {
			continue
		}
		data = append(data, map[string]any{"name": s.Label, "value": s.Value})
	}
	option := map[string]any{
		"color":   donutPalette,
		"tooltip": map[string]any{"trigger": "item"},
		"legend": map[string]any{
			"show":          true,
			"type":          "scroll",
			"top":           0,
			"itemWidth":     8,
			"itemHeight":    8,
			"icon":          "circle",
			"textStyle":     map[string]any{"color": "var(--muted-foreground)", "fontSize": 11},
			"pageTextStyle": map[string]any{"color": "var(--muted-foreground)"},
		},
		"series": []map[string]any{{
			"name":      "split",
			"type":      "pie",
			"radius":    []string{"50%", "70%"},
			"center":    []string{"50%", "56%"},
			"itemStyle": map[string]any{"borderColor": "var(--card)", "borderWidth": 2},
			// The legend names the slices. Outside labels as well collided
			// with the legend and were clipped at the edge of narrow cards.
			"label": map[string]any{"show": false},
			"data":  data,
		}},
	}
	return json.Marshal(option)
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
		// The bottom margin has to clear two things stacked under the plot:
		// the date labels, and the legend sitting on the floor of the canvas.
		// At 28 they landed in the same strip and overlapped.
		"grid": map[string]any{"left": 44, "right": 12, "top": 28, "bottom": 64},
		"tooltip": map[string]any{
			"trigger":     "axis",
			"axisPointer": map[string]any{"type": "shadow"},
		},
		"legend": map[string]any{
			"show":       true,
			"bottom":     4,
			"itemWidth":  8,
			"itemHeight": 8,
			"textStyle":  map[string]any{"color": "var(--muted-foreground)", "fontSize": 10},
		},
		"xAxis": map[string]any{
			"type":      "category",
			"data":      labels,
			"axisLine":  map[string]any{"lineStyle": map[string]any{"color": "var(--border)"}},
			"axisTick":  map[string]any{"show": false},
			"axisLabel": map[string]any{"color": "var(--muted-foreground)", "fontSize": 10, "interval": "auto", "margin": 10},
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
