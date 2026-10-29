package insights

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/users"
)

// FocusSource is the week's focus from the weekly review, as lines; none
// when the week was not reviewed. weekly.Service satisfies it.
type FocusSource interface {
	FocusLines(ctx context.Context, user users.User, weekStart time.Time) ([]string, error)
}

// WithFocus lets the scoreboard show what this week is for beside how each
// area is going. Wired after construction: the weekly service is built later.
func (a *API) WithFocus(f FocusSource) *API {
	a.focus = f
	return a
}

type AreaPointView struct {
	Points  int  `json:"points"`
	HasData bool `json:"hasData"`
}

type AreaView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Points, verdict and reason are this week so far, as the summary
	// words them; verdict is empty when there is too little to judge.
	Points  int    `json:"points"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
	HasData bool   `json:"hasData"`
	// Trend is one point per week, oldest first, matching weeks.
	Trend []AreaPointView `json:"trend"`
}

type AreasView struct {
	// Weeks are the Mondays of each week, YYYY-MM-DD, oldest first; the last
	// is the week in progress.
	Weeks []string   `json:"weeks"`
	Areas []AreaView `json:"areas"`
	// Focus is this week's focus from the weekly review; empty when the week
	// was not reviewed.
	Focus []string `json:"focus"`
}

func (a *API) areas(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	weeks, _ := strconv.Atoi(c.QueryParam("weeks"))
	now := time.Now()
	data, err := a.svc.AreaTrend(ctx, user, weeks, now)
	if err != nil {
		return httpx.Fail(err, "Areas could not be loaded.")
	}
	var focus []string
	if a.focus != nil && len(data.Weeks) > 0 {
		if focus, err = a.focus.FocusLines(ctx, user, data.Weeks[len(data.Weeks)-1]); err != nil {
			return httpx.Fail(err, "Areas could not be loaded.")
		}
	}
	view, err := projectAreas(ctx, data, focus)
	if err != nil {
		return httpx.Fail(err, "Areas could not be loaded.")
	}
	return c.JSON(http.StatusOK, view)
}

func projectAreas(ctx context.Context, data Areas, focus []string) (AreasView, error) {
	out := AreasView{Weeks: make([]string, 0, len(data.Weeks)), Areas: make([]AreaView, 0, len(data.Areas)), Focus: nonNil(focus)}
	for _, monday := range data.Weeks {
		out.Weeks = append(out.Weeks, monday.Format(time.DateOnly))
	}
	for i, area := range data.Areas {
		card, err := scoreCard(ctx, domains[i], area.Now)
		if err != nil {
			return AreasView{}, err
		}
		v := AreaView{
			Key: area.Key, Label: area.Label, Points: card.Points, Verdict: card.Verdict, Reason: card.Reason,
			HasData: card.HasData, Trend: make([]AreaPointView, 0, len(area.Trend)),
		}
		for _, p := range area.Trend {
			v.Trend = append(v.Trend, AreaPointView(p))
		}
		out.Areas = append(out.Areas, v)
	}
	return out, nil
}
