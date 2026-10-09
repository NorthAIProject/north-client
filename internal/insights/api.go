package insights

import (
	"net/http"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

// API is the insights overview, the domain pages and metric detail for
// native clients.
//
// It projects the web's own view models rather than recomputing them: a score,
// a verdict or a trend word that differed between the phone and the browser
// would make both untrustworthy. Charts arrive as labelled series, which is
// all a native chart needs; colours and styling stay with each client.
type API struct {
	svc   *Service
	focus FocusSource
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/insights", a.summary)
	r.Get("/insights/metrics/{key}", a.metric)
	r.Get("/insights/health", a.health)
	r.Get("/insights/timeline", a.timeline)
	r.Get("/insights/body", a.body)
	r.Get("/insights/mind", a.mind)
	r.Get("/insights/progress", a.progress)
	r.Get("/insights/training", a.training)
	r.Get("/insights/nutrition", a.nutrition)
	r.Get("/insights/coach", a.coach)
	r.Get("/insights/spend", a.spend)
	r.Get("/insights/areas", a.areas)
}

type Range struct {
	Key     string        `json:"key"`
	Label   string        `json:"label"`
	Options []RangeOption `json:"options"`
}

type RangeOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Chart is one or more series over shared labels (dates, usually).
type Chart struct {
	Labels []string      `json:"labels"`
	Series []ChartSeries `json:"series"`
}

type ChartSeries struct {
	Label  string    `json:"label"`
	Values []float64 `json:"values"`
}

type Delta struct {
	// Direction is 1 up, -1 down, 0 flat; whether up is good depends on the
	// metric, which the verdict and trend word already say.
	Direction int     `json:"direction"`
	Pct       float64 `json:"pct"`
	HasPrior  bool    `json:"hasPrior"`
}

type ScoreComponent struct {
	Label  string `json:"label"`
	Earned int    `json:"earned"`
	Weight int    `json:"weight"`
	Known  bool   `json:"known"`
}

type Score struct {
	// Key is body, mind, progress, training or nutrition.
	Key     string `json:"key"`
	Label   string `json:"label"`
	Points  int    `json:"points"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
	// Coverage is the share of the score's inputs that had data, 0-100.
	Coverage   int              `json:"coverage"`
	HasData    bool             `json:"hasData"`
	Components []ScoreComponent `json:"components"`
}

type Pinned struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Note  string `json:"note"`
	// MetricKey opens the metric's detail; empty when the card is not one.
	MetricKey string `json:"metricKey,omitempty"`
	Delta     Delta  `json:"delta"`
	Chart     *Chart `json:"chart,omitempty"`
}

type Summary struct {
	Range      Range    `json:"range"`
	Scores     []Score  `json:"scores"`
	Pinned     []Pinned `json:"pinned"`
	Highlights []string `json:"highlights"`
	// OnTrack of Judged domains score OK or better. Deliberately not blended
	// into one number; see SummaryView.
	OnTrack int `json:"onTrack"`
	Judged  int `json:"judged"`
	// Empty means nothing is logged anywhere yet.
	Empty bool `json:"empty"`
}

type Trend struct {
	Direction int     `json:"direction"`
	Pct       float64 `json:"pct"`
	Word      string  `json:"word"`
	HasPrior  bool    `json:"hasPrior"`
}

type Comparison struct {
	CurrentLabel string `json:"currentLabel"`
	CurrentValue string `json:"currentValue"`
	CurrentPct   int    `json:"currentPct"`
	PriorLabel   string `json:"priorLabel"`
	PriorValue   string `json:"priorValue"`
	PriorPct     int    `json:"priorPct"`
	HasPrior     bool   `json:"hasPrior"`
}

type MetricDetail struct {
	Range      Range       `json:"range"`
	Key        string      `json:"key"`
	Label      string      `json:"label"`
	Headline   string      `json:"headline"`
	Note       string      `json:"note"`
	Chart      Chart       `json:"chart"`
	Trend      Trend       `json:"trend"`
	Comparison Comparison  `json:"comparison"`
	Highlights []string    `json:"highlights"`
	HasData    bool        `json:"hasData"`
	Health     bool        `json:"health,omitempty"`
	Usual      *UsualRange `json:"usual,omitempty"`
}

// UsualRange is a latest day against the person's usual range. Text is the
// sentence to show; the numbers are there for a client that draws the band.
type UsualRange struct {
	Day    string  `json:"day"`
	Latest float64 `json:"latest"`
	Mean   float64 `json:"mean"`
	SD     float64 `json:"sd"`
	Low    string  `json:"low"`
	High   string  `json:"high"`
	Days   int     `json:"days"`
	Z      float64 `json:"z"`
	// State is "above", "usual" or "below", kept an open string so a new
	// state never breaks a shipped client's decode.
	State string `json:"state"`
	Text  string `json:"text"`
}

// HealthList is the health metrics a person has recent readings for.
type HealthList struct {
	Metrics []HealthMetric `json:"metrics"`
}

// HealthMetric is one row of the health list. Recent holds the last
// fortnight's daily values, oldest first, for a sparkline.
type HealthMetric struct {
	Key    string      `json:"key"`
	Label  string      `json:"label"`
	Latest string      `json:"latest"`
	Day    string      `json:"day"`
	Recent []float64   `json:"recent"`
	Usual  *UsualRange `json:"usual,omitempty"`
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	rg := timerange.Parse(r.URL.Query().Get("range"), user.Location())
	data, err := a.svc.Summary(r.Context(), user, rg)
	if err != nil {
		httpx.Error(w, err, "Insights could not be loaded.")
		return
	}
	view, err := buildSummaryView(data)
	if err != nil {
		httpx.Error(w, err, "Insights could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectSummary(view))
}

func (a *API) metric(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	rg := timerange.Parse(r.URL.Query().Get("range"), user.Location())
	data, err := a.svc.Metric(r.Context(), user, rg, chi.URLParam(r, "key"))
	if err != nil {
		httpx.Error(w, err, "That metric could not be loaded.")
		return
	}
	view, err := buildMetricView(data)
	if err != nil {
		httpx.Error(w, err, "That metric could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectMetric(view))
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	rows, err := a.svc.Health(r.Context(), user, time.Now())
	if err != nil {
		httpx.Error(w, err, "Health could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectHealth(rows))
}

func projectHealth(rows []HealthRow) HealthList {
	out := HealthList{Metrics: []HealthMetric{}}
	for _, row := range rows {
		latest := row.Recent[len(row.Recent)-1]
		metric := HealthMetric{
			Key: row.Metric.Key, Label: row.Metric.Label,
			Latest: formatMetric(row.Metric, latest.Value), Day: latest.At.Format(time.DateOnly),
			Recent: make([]float64, 0, len(row.Recent)),
		}
		for _, p := range row.Recent {
			metric.Recent = append(metric.Recent, p.Value)
		}
		if row.Usual != nil {
			metric.Usual = projectUsual(usualView(row.Metric, *row.Usual))
		}
		out.Metrics = append(out.Metrics, metric)
	}
	return out
}

func projectUsual(v insightpages.UsualView) *UsualRange {
	return &UsualRange{
		Day: v.Day.Format(time.DateOnly), Latest: v.Latest, Mean: v.Mean, SD: v.SD,
		Low: v.Low, High: v.High, Days: v.Days, Z: v.Z, State: v.State, Text: v.Text,
	}
}

func projectSummary(v insightpages.SummaryView) Summary {
	out := Summary{
		Range: projectRange(v.Range), Scores: []Score{}, Pinned: []Pinned{},
		Highlights: nonNil(v.Highlights), OnTrack: v.OnTrack, Judged: v.Judged, Empty: v.Empty,
	}
	for _, s := range v.Scores {
		score := Score{
			Key: s.Key, Label: s.Label, Points: s.Points, Verdict: s.Verdict, Reason: s.Reason,
			Coverage: s.Coverage, HasData: s.HasData, Components: []ScoreComponent{},
		}
		for _, c := range s.Components {
			score.Components = append(score.Components, ScoreComponent{Label: c.Label, Earned: c.Earned, Weight: c.Weight, Known: c.Known})
		}
		out.Scores = append(out.Scores, score)
	}
	for _, p := range v.Pinned {
		pinned := Pinned{
			Label: p.Label, Value: p.Value, Note: p.Note, MetricKey: metricKeyFromHref(p.Href),
			Delta: Delta{Direction: p.Delta.Direction, Pct: p.Delta.Pct, HasPrior: p.Delta.HasPrior},
		}
		if p.HasChart {
			pinned.Chart = util.Ptr(projectChart(p.Chart.Data))
		}
		out.Pinned = append(out.Pinned, pinned)
	}
	return out
}

func projectMetric(v insightpages.MetricView) MetricDetail {
	out := MetricDetail{
		Range: projectRange(v.Range), Key: v.Key, Label: v.Label, Headline: v.Headline, Note: v.Note,
		Chart: projectChart(v.Chart.Data),
		Trend: Trend{Direction: v.Trend.Direction, Pct: v.Trend.Pct, Word: v.Trend.Word, HasPrior: v.Trend.HasPrior},
		Comparison: Comparison{
			CurrentLabel: v.Comparison.CurrentLabel, CurrentValue: v.Comparison.CurrentValue, CurrentPct: v.Comparison.CurrentPct,
			PriorLabel: v.Comparison.PriorLabel, PriorValue: v.Comparison.PriorValue, PriorPct: v.Comparison.PriorPct,
			HasPrior: v.Comparison.HasPrior,
		},
		Highlights: nonNil(v.Highlights),
		HasData:    v.HasData,
		Health:     v.Health,
	}
	if v.Usual != nil {
		out.Usual = projectUsual(*v.Usual)
	}
	return out
}

func projectRange(v insightpages.RangeView) Range {
	out := Range{Key: v.Key, Label: v.Label, Options: []RangeOption{}}
	for _, o := range v.Options {
		out.Options = append(out.Options, RangeOption{Key: o.Key, Label: o.Label})
	}
	return out
}

func projectChart(d chart.Data) Chart {
	out := Chart{Labels: nonNil(d.Labels), Series: []ChartSeries{}}
	for _, ds := range d.Datasets {
		values := ds.Data
		if values == nil {
			values = []float64{}
		}
		out.Series = append(out.Series, ChartSeries{Label: ds.Label, Values: values})
	}
	return out
}

// metricKeyFromHref reads the key out of a metric detail link, the only
// place a pinned card names its metric.
func metricKeyFromHref(href string) string {
	const prefix = "/app/insights/metric/"
	if !strings.HasPrefix(href, prefix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(href, prefix), "/")
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
