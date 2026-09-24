package care

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/sleep"
)

// API is the web /care page for native clients: water, last night's sleep,
// habits and reminders. Every change answers with the fresh page, so a client
// never recomputes a total, a streak or which reminders are due.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(opts Options) *API { return &API{svc: NewService(opts)} }

func (a *API) Routes(r chi.Router) {
	r.Get("/care", a.show)
	r.Post("/care/water", a.logWater)
	r.Delete("/care/water/{entryID}", a.undoWater)
	r.Put("/care/sleep", a.logSleep)
	r.Post("/care/habits", a.createHabit)
	r.Put("/care/habits/{habitID}/done", a.setHabitDone)
	r.Delete("/care/habits/{habitID}", a.deleteHabit)
	r.Post("/care/reminders", a.createReminder)
	r.Put("/care/reminders/{reminderID}/enabled", a.setReminderEnabled)
	r.Delete("/care/reminders/{reminderID}", a.deleteReminder)
}

type WaterEntryView struct {
	ID       uuid.UUID `json:"id"`
	AmountML int       `json:"amountMl"`
	LoggedAt time.Time `json:"loggedAt"`
}

type WaterView struct {
	TotalML  int              `json:"totalMl"`
	TargetML int              `json:"targetMl"`
	Entries  []WaterEntryView `json:"entries"`
}

type SleepView struct {
	// LocalDate is the morning the night ended.
	LocalDate       string `json:"localDate"`
	DurationMinutes int    `json:"durationMinutes"`
	// Quality is 1-5 when given.
	Quality  *int   `json:"quality,omitempty"`
	Bedtime  string `json:"bedtime,omitempty"`
	WakeTime string `json:"wakeTime,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

type HabitView struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Domain string    `json:"domain"`
	// DaysOfWeek are 0 (Sunday) to 6; at least one.
	DaysOfWeek     []int `json:"daysOfWeek"`
	Streak         int   `json:"streak"`
	Kept           int   `json:"kept"`
	Scheduled      int   `json:"scheduled"`
	DoneToday      bool  `json:"doneToday"`
	ScheduledToday bool  `json:"scheduledToday"`
}

type ReminderView struct {
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label"`
	// TimeOfDay is HH:MM in the person's zone.
	TimeOfDay  string `json:"timeOfDay"`
	DaysOfWeek []int  `json:"daysOfWeek"`
	Enabled    bool   `json:"enabled"`
	// Due means it has fired today and is waiting to be done.
	Due bool `json:"due"`
}

type CarePoint struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type CareView struct {
	Water          WaterView      `json:"water"`
	LastNight      *SleepView     `json:"lastNight,omitempty"`
	Habits         []HabitView    `json:"habits"`
	Reminders      []ReminderView `json:"reminders"`
	CheckedInToday bool           `json:"checkedInToday"`
	// The last seven days, oldest first.
	WaterWeek []CarePoint `json:"waterWeek"`
	SleepWeek []CarePoint `json:"sleepWeek"`
	// HabitRate is the share of scheduled habits kept this week, 0-100.
	HabitRate int `json:"habitRate"`
}

type AmountRequest struct {
	AmountML int `json:"amountMl"`
}

type SleepRequest struct {
	DurationMinutes int    `json:"durationMinutes"`
	Quality         *int   `json:"quality,omitempty"`
	Bedtime         string `json:"bedtime,omitempty"`
	WakeTime        string `json:"wakeTime,omitempty"`
	Notes           string `json:"notes,omitempty"`
}

type HabitRequest struct {
	Name       string `json:"name"`
	Domain     string `json:"domain"`
	DaysOfWeek []int  `json:"daysOfWeek"`
}

type ReminderRequest struct {
	Label      string `json:"label"`
	TimeOfDay  string `json:"timeOfDay"`
	DaysOfWeek []int  `json:"daysOfWeek"`
}

type FlagRequest struct {
	Value bool `json:"value"`
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	a.respond(w, r, http.StatusOK)
}

func (a *API) logWater(w http.ResponseWriter, r *http.Request) {
	var req AmountRequest
	if !readJSON(w, r, &req) {
		return
	}
	if _, err := a.svc.hydration.Log(r.Context(), auth.MustUser(r.Context()), req.AmountML); err != nil {
		httpx.Error(w, err, "The water could not be logged.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) undoWater(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "entryID")
	if !ok {
		return
	}
	if err := a.svc.hydration.Undo(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		httpx.Error(w, err, "That could not be undone.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) logSleep(w http.ResponseWriter, r *http.Request) {
	var req SleepRequest
	if !readJSON(w, r, &req) {
		return
	}
	if _, err := a.svc.sleep.LogToday(r.Context(), auth.MustUser(r.Context()), sleep.Input(req)); err != nil {
		httpx.Error(w, err, "The night could not be logged.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) createHabit(w http.ResponseWriter, r *http.Request) {
	var req HabitRequest
	if !readJSON(w, r, &req) {
		return
	}
	days := make([]time.Weekday, 0, len(req.DaysOfWeek))
	for _, d := range req.DaysOfWeek {
		days = append(days, time.Weekday(d))
	}
	if _, err := a.svc.habits.Create(r.Context(), auth.MustUser(r.Context()), habits.Input{
		Name: req.Name, Domain: req.Domain, Days: days, Active: true,
	}); err != nil {
		httpx.Error(w, renameField(err, "days", "daysOfWeek"), "The habit could not be saved.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) setHabitDone(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "habitID")
	if !ok {
		return
	}
	var req FlagRequest
	if !readJSON(w, r, &req) {
		return
	}
	user := auth.MustUser(r.Context())
	var err error
	if req.Value {
		err = a.svc.habits.Complete(r.Context(), user, id)
	} else {
		err = a.svc.habits.Uncomplete(r.Context(), user, id)
	}
	if err != nil {
		httpx.Error(w, err, "The habit could not be updated.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) deleteHabit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "habitID")
	if !ok {
		return
	}
	if err := a.svc.habits.Delete(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		httpx.Error(w, err, "The habit could not be deleted.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) createReminder(w http.ResponseWriter, r *http.Request) {
	var req ReminderRequest
	if !readJSON(w, r, &req) {
		return
	}
	if _, err := a.svc.reminders.Create(r.Context(), auth.MustUser(r.Context()).ID, meals.ReminderInput(req)); err != nil {
		httpx.Error(w, err, "The reminder could not be saved.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) setReminderEnabled(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "reminderID")
	if !ok {
		return
	}
	var req FlagRequest
	if !readJSON(w, r, &req) {
		return
	}
	if _, err := a.svc.reminders.Toggle(r.Context(), id, auth.MustUser(r.Context()).ID, req.Value); err != nil {
		httpx.Error(w, err, "The reminder could not be changed.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) deleteReminder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "reminderID")
	if !ok {
		return
	}
	if err := a.svc.reminders.Delete(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The reminder could not be deleted.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

// respond answers with the page as it now stands.
func (a *API) respond(w http.ResponseWriter, r *http.Request, status int) {
	snap, err := a.svc.Load(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		httpx.Error(w, err, "Care could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, project(snap))
}

func project(s Snapshot) CareView {
	out := CareView{
		Water:          WaterView{TotalML: s.Water.TotalML, TargetML: s.Water.TargetML, Entries: []WaterEntryView{}},
		Habits:         []HabitView{},
		Reminders:      []ReminderView{},
		CheckedInToday: s.CheckedInToday,
		WaterWeek:      points(s.HydrationSeries),
		SleepWeek:      points(s.SleepSeries),
		HabitRate:      s.HabitRate,
	}
	for _, e := range s.WaterEntries {
		out.Water.Entries = append(out.Water.Entries, WaterEntryView{ID: e.ID, AmountML: e.AmountML, LoggedAt: e.LoggedAt})
	}
	if s.SleptLastNight {
		n := s.LastNight
		out.LastNight = &SleepView{
			LocalDate: n.LocalDate.Format("2006-01-02"), DurationMinutes: n.DurationMinutes, Quality: n.Quality,
			Bedtime: n.Bedtime, WakeTime: n.WakeTime, Notes: n.Notes,
		}
	}
	for _, h := range s.Habits {
		days := make([]int, 0, len(h.Habit.Days))
		for _, d := range h.Habit.Days {
			days = append(days, int(d))
		}
		out.Habits = append(out.Habits, HabitView{
			ID: h.Habit.ID, Name: h.Habit.Name, Domain: h.Habit.Domain, DaysOfWeek: days,
			Streak: h.Streak, Kept: h.Kept, Scheduled: h.Scheduled, DoneToday: h.DoneToday, ScheduledToday: h.ScheduledToday,
		})
	}
	due := make(map[uuid.UUID]bool, len(s.DueReminders))
	for _, rem := range s.DueReminders {
		due[rem.ID] = true
	}
	for _, rem := range s.AllReminders {
		days := rem.DaysOfWeek
		if days == nil {
			days = []int{}
		}
		out.Reminders = append(out.Reminders, ReminderView{
			ID: rem.ID, Label: rem.Label, TimeOfDay: rem.TimeOfDay, DaysOfWeek: days, Enabled: rem.Enabled, Due: due[rem.ID],
		})
	}
	return out
}

func points(series []DayPoint) []CarePoint {
	out := make([]CarePoint, 0, len(series))
	for _, p := range series {
		out = append(out, CarePoint(p))
	}
	return out
}

// renameField reports a service field error under the name the API uses, so
// a client can put it next to the right control.
func renameField(err error, from, to string) error {
	var fields apperr.FieldErrors
	if !apperr.As(err, &fields) {
		return err
	}
	renamed := make(apperr.FieldErrors, 0, len(fields))
	for _, f := range fields {
		if f.Field == from {
			f.Field = to
		}
		renamed = append(renamed, f)
	}
	return renamed
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
