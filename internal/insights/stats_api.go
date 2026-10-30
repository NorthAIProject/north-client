package insights

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/insights/insightview"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
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

func (a *API) registerStats(g *echo.Group) {
	g.GET("/insights/sleep", a.sleepPage)
	g.GET("/insights/cardio", a.cardioPage)
	g.GET("/insights/patterns", a.patternsPage)
}

// statsContext is the reader, their window, and the stats reader; an error
// when stats are not wired, which the web answered with a 404 too.
func (a *API) statsContext(c *echo.Context) (StatsReader, timerange.Range, error) {
	user := auth.MustUser(c.Request().Context())
	rg := timerange.Parse(c.QueryParam("range"), user.Location())
	if a.svc.stats == nil {
		return nil, rg, httpx.Fail(apperr.ErrNotFound, domainError)
	}
	return a.svc.stats, rg, nil
}

func (a *API) sleepPage(c *echo.Context) error {
	stats, rg, err := a.statsContext(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	st, err := stats.Sleep(ctx, auth.MustUser(ctx), rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectSleep(buildSleepView(ctx, rg, st)))
}

func (a *API) cardioPage(c *echo.Context) error {
	stats, rg, err := a.statsContext(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	st, err := stats.Cardio(ctx, auth.MustUser(ctx), rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectCardio(buildCardioView(ctx, rg, st)))
}

func (a *API) patternsPage(c *echo.Context) error {
	stats, rg, err := a.statsContext(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	found, days, err := stats.Patterns(ctx, auth.MustUser(ctx), rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectPatterns(buildPatternsView(ctx, rg, days, found)))
}

func projectSleep(v insightview.SleepView) InsightsSleep {
	return InsightsSleep{
		Range: projectRange(v.Range), HasData: v.HasData, Tiles: projectRows(v.Tiles),
		Nights: projectChart(v.Nights.Data), Stages: projectBars(v.Stages),
		Schedule: projectRows(v.Schedule), Extremes: projectRows(v.Extremes),
	}
}

func projectCardio(v insightview.CardioView) InsightsCardio {
	return InsightsCardio{
		Range: projectRange(v.Range), HasData: v.HasData, Tiles: projectRows(v.Tiles),
		Weekly: projectChart(v.Weekly.Data), WeeklyTitle: v.WeeklyTitle, Kinds: projectBars(v.Kinds),
		Runs: projectRows(v.Runs), HasRuns: v.HasRuns, Recent: projectRows(v.Recent), Heart: projectRows(v.Heart),
	}
}

func projectPatterns(v insightview.PatternsView) InsightsPatterns {
	out := InsightsPatterns{Range: projectRange(v.Range), Days: v.Days, Findings: []InsightsPattern{}}
	for _, f := range v.Findings {
		out.Findings = append(out.Findings, InsightsPattern{Title: f.Title, Detail: f.Detail, A: projectBar(f.A), B: projectBar(f.B)})
	}
	return out
}

func projectRows(rows []insightview.Row) []StatsRow {
	out := []StatsRow{}
	for _, r := range rows {
		out = append(out, StatsRow{Label: r.Label, Value: r.Value, Note: r.Note})
	}
	return out
}

func projectBars(bars []insightview.Bar) []StatsBar {
	out := []StatsBar{}
	for _, b := range bars {
		out = append(out, projectBar(b))
	}
	return out
}

func projectBar(b insightview.Bar) StatsBar {
	return StatsBar{Label: b.Label, Value: b.Value, Pct: b.Pct}
}
