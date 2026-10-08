package workouts

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
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

func (a *API) showWeek(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	var (
		progress WeekProgress
		err      error
	)
	if wantsNextWeek(r) {
		progress, err = a.svc.NextWeek(r.Context(), user, time.Now())
	} else {
		progress, err = a.svc.WeekProgress(r.Context(), user, time.Now())
	}
	if err != nil {
		httpx.Error(w, err, "This week could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectWeek(progress))
}

func (a *API) setWeek(w http.ResponseWriter, r *http.Request) {
	change, ok := readWeekChange(w, r)
	if !ok {
		return
	}
	progress, err := a.svc.SetWeek(r.Context(), auth.MustUser(r.Context()), time.Now(), change)
	if err != nil {
		httpx.Error(w, err, "That week could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectWeek(progress))
}

func (a *API) suggestWeek(w http.ResponseWriter, r *http.Request) {
	change, ok := readWeekChange(w, r)
	if !ok {
		return
	}
	user := auth.MustUser(r.Context())
	progress, err := a.svc.SuggestWeek(r.Context(), user, time.Now(), change)
	if err != nil {
		httpx.Error(w, err, "No week could be suggested.")
		return
	}
	matching, err := a.svc.PlansWithDays(r.Context(), user.ID, len(progress.Days))
	if err != nil {
		httpx.Error(w, err, "No week could be suggested.")
		return
	}
	out := WeekSuggestion{Week: projectWeek(progress), MatchingPlans: make([]PlanSummary, 0, len(matching))}
	for _, p := range matching {
		out.MatchingPlans = append(out.MatchingPlans, projectSummary(p))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) resetWeek(w http.ResponseWriter, r *http.Request) {
	progress, err := a.svc.ResetWeek(r.Context(), auth.MustUser(r.Context()), time.Now(), wantsNextWeek(r))
	if err != nil {
		httpx.Error(w, err, "That week could not be reset.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectWeek(progress))
}

func (a *API) activatePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "planID")
	if !ok {
		return
	}
	active, err := a.svc.SetActivePlan(r.Context(), auth.MustUser(r.Context()), id)
	if err != nil {
		httpx.Error(w, err, "That plan could not be followed.")
		return
	}
	a.writePlan(w, r, active.ID, http.StatusOK)
}

func readWeekChange(w http.ResponseWriter, r *http.Request) (WeekChange, bool) {
	var req WeekRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 8 << 10}); err != nil {
		httpx.Error(w, err, "The request body must be a training week.")
		return WeekChange{}, false
	}
	change := WeekChange{Weekdays: req.Weekdays, Days: req.Days, PlanID: req.PlanID, NextWeek: wantsNextWeek(r)}
	if len(req.Assignments) > 0 {
		change.Assign = make(map[string]SessionRef, len(req.Assignments))
		for _, a := range req.Assignments {
			change.Assign[a.Weekday] = SessionRef{PlanID: a.PlanID, DayIndex: a.DayIndex}
		}
	}
	return change, true
}

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
