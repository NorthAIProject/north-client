package checkins

import (
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/moments"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is check-ins for native clients: today's, the recent ones, the streak,
// and editing, over the same service the web /check-ins page uses.
type API struct {
	svc *Service
	// funnel reports moments shown, as the web handler does. Nil is a no-op.
	funnel *analytics.Funnel
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

// WithFunnel reports the moments the app is sent.
func (a *API) WithFunnel(f *analytics.Funnel) *API {
	a.funnel = f
	return a
}

func (a *API) Routes(r chi.Router) {
	r.Get("/check-ins", a.list)
	r.Put("/check-ins/today", a.upsertToday)
	r.Put("/check-ins/{checkInID}", a.update)
	r.Delete("/check-ins/{checkInID}", a.destroy)
}

type CheckInView struct {
	ID uuid.UUID `json:"id"`
	// LocalDate is the day in the person's own time zone it belongs to.
	LocalDate  string `json:"localDate"`
	Mood       int    `json:"mood"`
	Energy     int    `json:"energy"`
	Wins       string `json:"wins"`
	Challenges string `json:"challenges"`
	Notes      string `json:"notes"`
	// RelatedGoalID and RelatedGoalTitle are set when it was about a goal.
	RelatedGoalID    *uuid.UUID `json:"relatedGoalId,omitempty"`
	RelatedGoalTitle string     `json:"relatedGoalTitle,omitempty"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	CreatedAt        time.Time  `json:"createdAt"`
	// Source is where the check-in was first created: web, ios, siri, coach,
	// mcp, capture, or unknown for entries from before it was recorded. Edits
	// do not change it. Open-ended; clients must tolerate values they do not
	// know.
	Source string `json:"source"`
	// Stress and SleepQuality are 1–5, absent when not given.
	Stress       *int `json:"stress,omitempty"`
	SleepQuality *int `json:"sleepQuality,omitempty"`
	// Tags are lowercase labels; empty, never absent.
	Tags []string `json:"tags"`
}

type CheckInList struct {
	// Today is absent until today's check-in exists.
	Today  *CheckInView  `json:"today,omitempty"`
	Recent []CheckInView `json:"recent"`
	// Streak is check-in days in the run ending today or yesterday. A rest
	// day bridges a missed day without adding to it.
	Streak int `json:"streak"`
	// BestStreak is the longest run in the person's recent history.
	BestStreak int `json:"bestStreak"`
	// RestDaysHeld is 0 to 2: one is earned per seven check-in days.
	RestDaysHeld int `json:"restDaysHeld"`
	// RestDaysUsed is how many the current run has spent.
	RestDaysUsed int `json:"restDaysUsed"`
	// PreviousRun is the length of the run before the current one; the web
	// names the best run on the first day of a new one after a long run.
	PreviousRun int `json:"previousRun"`
	// PerfectWeek is every day of this week so far checked in.
	PerfectWeek bool `json:"perfectWeek"`
	// Week is Monday to Sunday of the current week, always seven days.
	Week []WeekDayView `json:"week"`
}

// WeekDayView is one day of the current week.
type WeekDayView struct {
	Date string `json:"date"`
	// State is checked, rest, none, today or upcoming. Open-ended; clients
	// must tolerate values they do not know.
	State string `json:"state"`
}

// setRun fills the run fields from the person's run.
func (l *CheckInList) setRun(run checkin.Run) {
	l.Streak = run.Current
	l.BestStreak = run.Best
	l.RestDaysHeld = run.RestsHeld
	l.RestDaysUsed = run.RestsUsed
	l.PreviousRun = run.PreviousRun
	l.PerfectWeek = run.PerfectWeek
	l.Week = weekView(run.Week)
}

func weekView(week [7]checkin.WeekDay) []WeekDayView {
	out := make([]WeekDayView, len(week))
	for i, d := range week {
		out[i] = WeekDayView{Date: d.Date.Format("2006-01-02"), State: string(d.State)}
	}
	return out
}

// SavedCheckInView is today's check-in as a save returns it: the check-in,
// plus what the save meant for the run.
type SavedCheckInView struct {
	CheckInView
	// Created is true when this save made today's check-in; false is an edit.
	Created      bool `json:"created"`
	Streak       int  `json:"streak"`
	RestDaysHeld int  `json:"restDaysHeld"`
	PerfectWeek  bool `json:"perfectWeek"`
	// Moment is the recognition this save earned. Only ever on a creating
	// save, and absent on most of those.
	Moment *MomentView `json:"moment,omitempty"`
}

// MomentView is one recognition card.
type MomentView struct {
	// Kind is streak, perfect_week, rest_day_earned or comeback. Open-ended;
	// clients must tolerate values they do not know.
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func projectSaved(s Saved, m *moments.Moment) SavedCheckInView {
	out := SavedCheckInView{
		CheckInView:  project(s.CheckIn),
		Created:      s.Created,
		Streak:       s.Run.Current,
		RestDaysHeld: s.Run.RestsHeld,
		PerfectWeek:  s.Run.PerfectWeek,
	}
	if m != nil {
		out.Moment = &MomentView{Kind: m.Kind, Title: m.Title, Body: m.Body}
	}
	return out
}

type CheckInRequest struct {
	Mood          int        `json:"mood"`
	Energy        int        `json:"energy"`
	Wins          string     `json:"wins"`
	Challenges    string     `json:"challenges"`
	Notes         string     `json:"notes"`
	RelatedGoalID *uuid.UUID `json:"relatedGoalId,omitempty"`
	// Stress, SleepQuality and Tags are optional. A save is a full overwrite
	// of the content fields, so leaving one out clears it.
	Stress       *int     `json:"stress,omitempty"`
	SleepQuality *int     `json:"sleepQuality,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	// Source is "ios", "siri", "widget", "notification" or "web" (the
	// SvelteKit web app); anything else is recorded as "ios". It is stored
	// only when the request creates the day's check-in.
	Source string `json:"source,omitempty"`
}

// input turns a request into a service input, recording which native surface
// sent it.
func (req CheckInRequest) input() Input {
	source := checkin.SourceIOS
	switch s := checkin.Source(req.Source); s {
	case checkin.SourceSiri, checkin.SourceWidget, checkin.SourceNotification, checkin.SourceWeb:
		source = s
	}
	return Input{
		Mood: req.Mood, Energy: req.Energy,
		Wins: req.Wins, Challenges: req.Challenges, Notes: req.Notes,
		RelatedGoalID: req.RelatedGoalID,
		Stress:        req.Stress, SleepQuality: req.SleepQuality, Tags: req.Tags,
		Source: source,
	}
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	out := CheckInList{Recent: []CheckInView{}}

	today, err := a.svc.Today(r.Context(), user)
	switch {
	case err == nil:
		out.Today = util.Ptr(project(today))
	case !apperr.Is(err, apperr.ErrNotFound):
		httpx.Error(w, err, "Check-ins could not be loaded.")
		return
	}

	recent, err := a.svc.List(r.Context(), user.ID, 30)
	if err != nil {
		httpx.Error(w, err, "Check-ins could not be loaded.")
		return
	}
	for _, c := range recent {
		out.Recent = append(out.Recent, project(c))
	}
	run, err := a.svc.Run(r.Context(), user)
	if err != nil {
		httpx.Error(w, err, "Check-ins could not be loaded.")
		return
	}
	out.setRun(run)
	httpx.WriteJSON(w, http.StatusOK, out)
}

// upsertToday is the one check-in a day: filing it again replaces its content,
// as the web form does, and keeps the source it was first created from.
func (a *API) upsertToday(w http.ResponseWriter, r *http.Request) {
	var req CheckInRequest
	if !readJSON(w, r, &req) {
		return
	}
	user := auth.MustUser(r.Context())
	saved, err := a.svc.UpsertToday(r.Context(), user, req.input())
	if err != nil {
		httpx.Error(w, err, "The check-in could not be saved.")
		return
	}
	var moment *moments.Moment
	if m, ok := moments.ForCheckIn(r.Context(), saved.Run, saved.Created); ok {
		a.funnel.MomentShown(r.Context(), user.ID, m.Kind)
		moment = &m
	}
	httpx.WriteJSON(w, http.StatusOK, projectSaved(saved, moment))
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "checkInID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	var req CheckInRequest
	if !readJSON(w, r, &req) {
		return
	}
	saved, err := a.svc.Update(r.Context(), id, auth.MustUser(r.Context()).ID, req.input())
	if err != nil {
		httpx.Error(w, err, "The check-in could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(saved))
}

func (a *API) destroy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "checkInID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	if err := a.svc.Delete(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The check-in could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func project(c CheckIn) CheckInView {
	return CheckInView{
		ID: c.ID, LocalDate: c.LocalDate.Format("2006-01-02"), Mood: c.Mood, Energy: c.Energy,
		Wins: c.Wins, Challenges: c.Challenges, Notes: c.Notes,
		RelatedGoalID: c.RelatedGoalID, RelatedGoalTitle: c.RelatedGoalTitle, UpdatedAt: c.UpdatedAt,
		CreatedAt: c.CreatedAt, Source: sourceLabel(c.Source),
		Stress: c.Stress, SleepQuality: c.SleepQuality, Tags: tagsOrEmpty(c.Tags),
	}
}

func sourceLabel(s checkin.Source) string {
	if s == "" {
		return string(checkin.SourceUnknown)
	}
	return string(s)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
