package dashboard

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// OverviewResponse is the web overview page (/app/overview): the same
// snapshot /today carries, plus what only the web page draws — the charts'
// series, the plan's next session and the check-in version its live region
// polls with. A separate route so /today, which the iOS app decodes, keeps
// its shape.
type OverviewResponse struct {
	Snapshot TodaySnapshot `json:"snapshot"`
	// NewsTickerEnabled renders the breaking-news strip.
	NewsTickerEnabled bool `json:"newsTickerEnabled"`
	// CheckInVersion fingerprints the person's check-ins; see /overview/version.
	CheckInVersion string `json:"checkInVersion"`
	// PlanID is the active training plan, absent without one.
	PlanID *uuid.UUID `json:"planId,omitempty"`
	// NextSession is the plan's next training day; DoneToday is today's
	// session once finished (NextSession has then moved past it).
	NextSession *OverviewPlanDay `json:"nextSession,omitempty"`
	DoneToday   *OverviewPlanDay `json:"doneToday,omitempty"`
	// SessionToday is NextSession falling on the reader's local today.
	SessionToday bool `json:"sessionToday"`
	// CheckIns is mood and energy per bucket of the chart window; zero is
	// no check-in.
	CheckIns []OverviewCheckInDay `json:"checkIns"`
	Habits   OverviewHabits       `json:"habits"`
	// HydrationDays is water per bucket of the chart window.
	HydrationDays []OverviewHydrationDay `json:"hydrationDays"`
}

// OverviewPlanDay is one day of the training plan.
type OverviewPlanDay struct {
	// Weekday is the plan's English weekday name ("Monday").
	Weekday string `json:"weekday"`
	Focus   string `json:"focus"`
	// StartTime is "HH:MM" local, empty for whenever.
	StartTime     string `json:"startTime,omitempty"`
	ExerciseCount int    `json:"exerciseCount"`
}

type OverviewCheckInDay struct {
	Label  string `json:"label"`
	Mood   int    `json:"mood"`
	Energy int    `json:"energy"`
}

type OverviewHabits struct {
	HasHabits  bool `json:"hasHabits"`
	Rate       int  `json:"rate"`
	Kept       int  `json:"kept"`
	Scheduled  int  `json:"scheduled"`
	BestStreak int  `json:"bestStreak"`
}

type OverviewHydrationDay struct {
	Label    string `json:"label"`
	TotalML  int    `json:"totalML"`
	TargetML int    `json:"targetML"`
}

// OverviewVersion is the cheap poll behind the overview's live Today region.
type OverviewVersion struct {
	CheckInVersion string `json:"checkInVersion"`
}

func (a *API) overview(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)

	rg := timerange.Parse(c.QueryParam("range"), user.Location())
	snapshot, err := a.svc.Load(ctx, user, rg)
	if err != nil {
		return httpx.Fail(err, "The overview could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectOverview(ctx, snapshot))
}

// version answers whether the check-ins moved, for one small query instead of
// the dozen a full overview load runs.
func (a *API) version(c *echo.Context) error {
	ctx := c.Request().Context()
	v, err := a.svc.CheckInVersion(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, "The overview could not be loaded.")
	}
	return c.JSON(http.StatusOK, OverviewVersion{CheckInVersion: v})
}

func projectOverview(ctx context.Context, snap Snapshot) OverviewResponse {
	out := OverviewResponse{
		Snapshot:          projectSnapshot(ctx, snap),
		NewsTickerEnabled: snap.NewsTickerEnabled,
		CheckInVersion:    snap.CheckInVersion,
		NextSession:       projectPlanDay(snap.NextSession),
		DoneToday:         projectPlanDay(snap.DoneToday),
		SessionToday:      snap.SessionToday,
		CheckIns:          make([]OverviewCheckInDay, len(snap.CheckIns.Days)),
		Habits: OverviewHabits{
			HasHabits: snap.Habits.HasHabits, Rate: snap.Habits.Rate, Kept: snap.Habits.Kept,
			Scheduled: snap.Habits.Scheduled, BestStreak: snap.Habits.BestStreak,
		},
		HydrationDays: make([]OverviewHydrationDay, len(snap.Hydration.Days)),
	}
	if snap.PlanID != uuid.Nil {
		id := snap.PlanID
		out.PlanID = &id
	}
	for i, d := range snap.CheckIns.Days {
		out.CheckIns[i] = OverviewCheckInDay(d)
	}
	for i, d := range snap.Hydration.Days {
		out.HydrationDays[i] = OverviewHydrationDay(d)
	}
	return out
}

func projectPlanDay(d *plan.PlanDay) *OverviewPlanDay {
	if d == nil {
		return nil
	}
	return &OverviewPlanDay{Weekday: d.Weekday, Focus: d.Focus, StartTime: d.StartTime, ExerciseCount: len(d.Exercises)}
}
