package workouts

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// The week a native client shows and changes: which days train, and what.
// ?week=next addresses next week instead of this one.

type TrainingWeek struct {
	// WeekStart is the week's Monday, YYYY-MM-DD.
	WeekStart string `json:"weekStart"`
	// Custom is a week someone changed rather than the plan's usual week.
	Custom     bool          `json:"custom"`
	WeekVolume string        `json:"weekVolume"`
	Days       []WeekSession `json:"days"`
	// Next is the session to train next, which may be next week's.
	Next *WeekSession `json:"next,omitempty"`
}

type WeekSession struct {
	Weekday string `json:"weekday"`
	// Date is YYYY-MM-DD.
	Date string `json:"date"`
	// PlanID and DayIndex locate the session: GET /training/plans/{planId}
	// and its days[dayIndex]. Start a guided workout for it with planWeekday
	// set to weekday, not to the plan day's own weekday.
	PlanID        uuid.UUID `json:"planId"`
	PlanName      string    `json:"planName"`
	DayIndex      int       `json:"dayIndex"`
	Focus         string    `json:"focus"`
	StartTime     string    `json:"startTime,omitempty"`
	ExerciseCount int       `json:"exerciseCount"`
	Completed     bool      `json:"completed"`
	IsNext        bool      `json:"isNext"`
}

type WeekRequest struct {
	// Weekdays are the days to train. Empty with days set asks for suggested
	// days; both empty is a rest week.
	Weekdays []string `json:"weekdays"`
	Days     int      `json:"days,omitempty"`
	// PlanID is the plan whose sessions fill the days; absent is the plan
	// being followed.
	PlanID *uuid.UUID `json:"planId,omitempty"`
	// Assignments pin a day to a session of any saved plan.
	Assignments []WeekAssignment `json:"assignments,omitempty"`
}

type WeekAssignment struct {
	Weekday  string    `json:"weekday"`
	PlanID   uuid.UUID `json:"planId"`
	DayIndex int       `json:"dayIndex"`
}

type WeekSuggestion struct {
	Week TrainingWeek `json:"week"`
	// MatchingPlans are saved plans written for exactly the chosen number of
	// days, to offer instead.
	MatchingPlans []PlanSummary `json:"matchingPlans"`
}

func (a *API) showWeek(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var (
		progress WeekProgress
		err      error
	)
	if wantsNextWeek(c.Request()) {
		progress, err = a.svc.NextWeek(ctx, user, time.Now())
	} else {
		progress, err = a.svc.WeekProgress(ctx, user, time.Now())
	}
	if err != nil {
		return httpx.Fail(err, "This week could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectWeek(progress))
}

func (a *API) setWeek(c *echo.Context) error {
	change, err := readWeekChange(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	progress, err := a.svc.SetWeek(ctx, auth.MustUser(ctx), time.Now(), change)
	if err != nil {
		return httpx.Fail(err, "That week could not be saved.")
	}
	return c.JSON(http.StatusOK, projectWeek(progress))
}

func (a *API) suggestWeek(c *echo.Context) error {
	change, err := readWeekChange(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	progress, err := a.svc.SuggestWeek(ctx, user, time.Now(), change)
	if err != nil {
		return httpx.Fail(err, "No week could be suggested.")
	}
	matching, err := a.svc.PlansWithDays(ctx, user.ID, len(progress.Days))
	if err != nil {
		return httpx.Fail(err, "No week could be suggested.")
	}
	out := WeekSuggestion{Week: projectWeek(progress), MatchingPlans: make([]PlanSummary, 0, len(matching))}
	for _, p := range matching {
		out.MatchingPlans = append(out.MatchingPlans, projectSummary(p))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) resetWeek(c *echo.Context) error {
	ctx := c.Request().Context()
	progress, err := a.svc.ResetWeek(ctx, auth.MustUser(ctx), time.Now(), wantsNextWeek(c.Request()))
	if err != nil {
		return httpx.Fail(err, "That week could not be reset.")
	}
	return c.JSON(http.StatusOK, projectWeek(progress))
}

func (a *API) activatePlan(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "planID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	active, err := a.svc.SetActivePlan(ctx, auth.MustUser(ctx), id)
	if err != nil {
		return httpx.Fail(err, "That plan could not be followed.")
	}
	return a.writePlan(c, active.ID, http.StatusOK)
}

func readWeekChange(c *echo.Context) (WeekChange, error) {
	var req WeekRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 8 << 10}, "The request body must be a training week."); err != nil {
		return WeekChange{}, err
	}
	change := WeekChange{Weekdays: req.Weekdays, Days: req.Days, PlanID: req.PlanID, NextWeek: wantsNextWeek(c.Request())}
	if len(req.Assignments) > 0 {
		change.Assign = make(map[string]SessionRef, len(req.Assignments))
		for _, a := range req.Assignments {
			change.Assign[a.Weekday] = SessionRef{PlanID: a.PlanID, DayIndex: a.DayIndex}
		}
	}
	return change, nil
}

// wantsNextWeek is shared with the browser week page.
func wantsNextWeek(r *http.Request) bool {
	return r.URL.Query().Get("week") == "next"
}

func projectWeek(p WeekProgress) TrainingWeek {
	out := TrainingWeek{
		WeekStart:  p.Start.Format(time.DateOnly),
		Custom:     p.Custom,
		WeekVolume: string(weekVolume(p.Volume)),
		Days:       make([]WeekSession, 0, len(p.Days)),
	}
	for _, d := range p.Days {
		session := projectSession(d)
		session.IsNext = p.HasNext && p.NextDay.Date.Equal(d.Date)
		out.Days = append(out.Days, session)
	}
	if p.HasNext {
		next := projectSession(p.NextDay)
		next.IsNext = true
		out.Next = &next
	}
	return out
}

func projectSession(d WeekDay) WeekSession {
	return WeekSession{
		Weekday: d.Weekday, Date: d.Date.Format(time.DateOnly),
		PlanID: d.PlanID, PlanName: d.PlanName, DayIndex: d.DayIndex,
		Focus: d.Day.Focus, StartTime: d.Day.StartTime, ExerciseCount: len(d.Day.Exercises),
		Completed: d.Completed,
	}
}
