package workouts

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/workouts/workoutsummary"
)

// WithRecaps lets GET /training/overview carry this week's latest workout.
func (a *API) WithRecaps(recaps Recaps) *API {
	a.recaps = recaps
	return a
}

// WithLoads lets GET /training/overview carry how ready each muscle is.
func (a *API) WithLoads(loads Loads) *API {
	a.loads = loads
	return a
}

// TrainingOverview is what the plan page shows above the plan: this week's
// latest finished workout and the body lit from logged sets. Each is absent
// until there is something to show (the web page's Recap and Readiness).
type TrainingOverview struct {
	Recap     *lifts.LiftRecapView `json:"recap,omitempty"`
	Readiness *TrainingReadiness   `json:"readiness,omitempty"`
}

// TrainingReadiness is the muscle map in three tiers, and the muscles going
// stale with how many days since they were worked.
type TrainingReadiness struct {
	Fatigued    []string      `json:"fatigued"`
	Recovering  []string      `json:"recovering"`
	Fresh       []string      `json:"fresh"`
	Stale       []StaleMuscle `json:"stale"`
	LastSession time.Time     `json:"lastSession"`
}

type StaleMuscle struct {
	Name string `json:"name"`
	Days int    `json:"days"`
}

func (a *API) overview(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	now := time.Now().In(user.Location())
	out := TrainingOverview{}

	if a.recaps != nil {
		progress, err := a.svc.WeekProgress(ctx, user, now)
		if err != nil {
			return httpx.Fail(err, "Training could not be loaded.")
		}
		if len(progress.Completed) > 0 {
			week := activity.WeekStart(now, user.Location())
			recap, ok, err := a.recaps.LatestRecap(ctx, user, timerange.Between(week, week.AddDate(0, 0, 7)))
			if err != nil {
				return httpx.Fail(err, "Training could not be loaded.")
			}
			if ok {
				view := lifts.ProjectRecap(recap)
				view.Sentence = recap.SentenceIn(i18n.LocaleFrom(ctx))
				out.Recap = &view
			}
		}
	}

	if a.loads != nil {
		load, err := a.loads.Readiness(ctx, user)
		if err != nil {
			return httpx.Fail(err, "Training could not be loaded.")
		}
		if !load.LastSession.IsZero() {
			rd := workoutsummary.NewReadiness(load)
			view := TrainingReadiness{
				Fatigued: nonNil(rd.Fatigued), Recovering: nonNil(rd.Recovering), Fresh: nonNil(rd.Fresh),
				Stale: make([]StaleMuscle, 0, len(rd.Stale)), LastSession: rd.LastSession,
			}
			for _, s := range rd.Stale {
				view.Stale = append(view.Stale, StaleMuscle{Name: s.Name, Days: s.Days})
			}
			out.Readiness = &view
		}
	}

	return c.JSON(http.StatusOK, out)
}
