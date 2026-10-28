package insights

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// The sleep, cardio and patterns pages as the web shows them: the same view
// models the templ pages render (statsPage), projected to JSON. /stats/* stays
// the raw numbers; these are the localised rows, bars and chart a page draws.

// StatsRow is a label and a value, with an optional note under the value.
type StatsRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Note  string `json:"note"`
}

// StatsBar is one horizontal bar: a label, a value, and its width in percent.
type StatsBar struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Pct   int    `json:"pct"`
}

type InsightsSleep struct {
	Range    Range      `json:"range"`
	HasData  bool       `json:"hasData"`
	Tiles    []StatsRow `json:"tiles"`
	Nights   Chart      `json:"nights"`
	Stages   []StatsBar `json:"stages"`
	Schedule []StatsRow `json:"schedule"`
	Extremes []StatsRow `json:"extremes"`
}

type InsightsCardio struct {
	Range       Range      `json:"range"`
	HasData     bool       `json:"hasData"`
	Tiles       []StatsRow `json:"tiles"`
	Weekly      Chart      `json:"weekly"`
	WeeklyTitle string     `json:"weeklyTitle"`
	Kinds       []StatsBar `json:"kinds"`
	Runs        []StatsRow `json:"runs"`
	HasRuns     bool       `json:"hasRuns"`
	Recent      []StatsRow `json:"recent"`
	Heart       []StatsRow `json:"heart"`
}

type InsightsPattern struct {
	Title  string   `json:"title"`
	Detail string   `json:"detail"`
	A      StatsBar `json:"a"`
	B      StatsBar `json:"b"`
}

type InsightsPatterns struct {
	Range    Range             `json:"range"`
	Days     int               `json:"days"`
	Findings []InsightsPattern `json:"findings"`
}

func (a *API) statsRoutes(r chi.Router) {
	r.Get("/insights/sleep", a.sleepPage)
	r.Get("/insights/cardio", a.cardioPage)
	r.Get("/insights/patterns", a.patternsPage)
}

// statsContext is the reader, their window, and the stats reader; false when
// stats are not wired, which the web answered with a 404 too.
func (a *API) statsContext(w http.ResponseWriter, r *http.Request) (StatsReader, timerange.Range, bool) {
	user := auth.MustUser(r.Context())
	rg := timerange.Parse(r.URL.Query().Get("range"), user.Location())
	if a.svc.stats == nil {
		httpx.Error(w, apperr.ErrNotFound, domainError)
		return nil, rg, false
	}
	return a.svc.stats, rg, true
}

func (a *API) sleepPage(w http.ResponseWriter, r *http.Request) {
	stats, rg, ok := a.statsContext(w, r)
	if !ok {
		return
	}
	st, err := stats.Sleep(r.Context(), auth.MustUser(r.Context()), rg)
	if err != nil {
		httpx.Error(w, err, domainError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectSleep(buildSleepView(r.Context(), rg, st)))
}

func (a *API) cardioPage(w http.ResponseWriter, r *http.Request) {
	stats, rg, ok := a.statsContext(w, r)
	if !ok {
		return
	}
	st, err := stats.Cardio(r.Context(), auth.MustUser(r.Context()), rg)
	if err != nil {
		httpx.Error(w, err, domainError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectCardio(buildCardioView(r.Context(), rg, st)))
}

func (a *API) patternsPage(w http.ResponseWriter, r *http.Request) {
	stats, rg, ok := a.statsContext(w, r)
	if !ok {
		return
	}
	found, days, err := stats.Patterns(r.Context(), auth.MustUser(r.Context()), rg)
	if err != nil {
		httpx.Error(w, err, domainError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectPatterns(buildPatternsView(r.Context(), rg, days, found)))
}

func projectSleep(v insightpages.SleepView) InsightsSleep {
	return InsightsSleep{
		Range: projectRange(v.Range), HasData: v.HasData, Tiles: projectRows(v.Tiles),
		Nights: projectChart(v.Nights.Data), Stages: projectBars(v.Stages),
		Schedule: projectRows(v.Schedule), Extremes: projectRows(v.Extremes),
	}
}

func projectCardio(v insightpages.CardioView) InsightsCardio {
	return InsightsCardio{
		Range: projectRange(v.Range), HasData: v.HasData, Tiles: projectRows(v.Tiles),
		Weekly: projectChart(v.Weekly.Data), WeeklyTitle: v.WeeklyTitle, Kinds: projectBars(v.Kinds),
		Runs: projectRows(v.Runs), HasRuns: v.HasRuns, Recent: projectRows(v.Recent), Heart: projectRows(v.Heart),
	}
}

func projectPatterns(v insightpages.PatternsView) InsightsPatterns {
	out := InsightsPatterns{Range: projectRange(v.Range), Days: v.Days, Findings: []InsightsPattern{}}
	for _, f := range v.Findings {
		out.Findings = append(out.Findings, InsightsPattern{Title: f.Title, Detail: f.Detail, A: projectBar(f.A), B: projectBar(f.B)})
	}
	return out
}

func projectRows(rows []insightpages.Row) []StatsRow {
	out := []StatsRow{}
	for _, r := range rows {
		out = append(out, StatsRow{Label: r.Label, Value: r.Value, Note: r.Note})
	}
	return out
}

func projectBars(bars []insightpages.Bar) []StatsBar {
	out := []StatsBar{}
	for _, b := range bars {
		out = append(out, projectBar(b))
	}
	return out
}

func projectBar(b insightpages.Bar) StatsBar {
	return StatsBar{Label: b.Label, Value: b.Value, Pct: b.Pct}
}
