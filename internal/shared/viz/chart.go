package viz

import "github.com/a-h/templ"

// The Chart.js chart description the builders in this package return.
//
// These were declared beside the templUI chart component in
// web/shared/ui/chart, which re-exports them under their old names. They live
// here so the JSON API, which embeds Props in several of its views, does not
// import the browser templates to describe a chart.

// Variant is a Chart.js chart type.
type Variant string

const (
	VariantBar      Variant = "bar"
	VariantLine     Variant = "line"
	VariantPie      Variant = "pie"
	VariantDoughnut Variant = "doughnut"
	VariantRadar    Variant = "radar"
)

// Dataset is one series of a Chart.js chart.
type Dataset struct {
	Label           string      `json:"label"`
	Data            []float64   `json:"data"`
	BorderWidth     int         `json:"borderWidth,omitempty"`
	BorderColor     interface{} `json:"borderColor,omitempty"`
	BackgroundColor interface{} `json:"backgroundColor,omitempty"`
	Tension         float64     `json:"tension,omitempty"`
	Fill            bool        `json:"fill,omitempty"`
	Stepped         bool        `json:"stepped,omitempty"`
}

// Options are the Chart.js options the component passes through.
type Options struct {
	Responsive bool `json:"responsive,omitempty"`
	Legend     bool `json:"legend,omitempty"`
}

// Data is a chart's labels and the series drawn over them.
type Data struct {
	Labels   []string  `json:"labels"`
	Datasets []Dataset `json:"datasets"`
}

// Props describes one chart. RawConfig, when set, is handed to Chart.js as is
// and the other fields only name and place the canvas.
type Props struct {
	ID          string
	Variant     Variant
	Data        Data
	Options     Options
	RawConfig   map[string]any
	ShowLegend  bool
	ShowXAxis   bool
	ShowYAxis   bool
	ShowXLabels bool
	ShowYLabels bool
	ShowXGrid   bool
	ShowYGrid   bool
	Horizontal  bool
	Stacked     bool
	YMin        *float64
	YMax        *float64
	BeginAtZero *bool
	Class       string
	Attributes  templ.Attributes
}
