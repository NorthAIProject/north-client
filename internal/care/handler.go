// Package care composes the daily accountability page: what's due, what's not
// done yet, and the quick logging that belongs to a person's day rather than
// to a training session — water, sleep and habits, alongside meal reminders
// and check-in status.
//
// It owns no repository. The slices it composes own their data; this package
// owns the question "what does today look like", which is a page rather than
// a domain.
//
// It has no ContextSource either: everything here already reaches the coach
// through its own slice's source, and a second heading would be noise rather
// than new information.
package care

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/sleep"
	carepages "github.com/NorthAIProject/north-client/web/care"
)

type Handler struct {
	reminders *meals.MealReminderService
	checkins  *checkins.Service
	hydration *hydration.Service
	sleep     *sleep.Service
	habits    *habits.Service
}

// Options keeps the constructor readable as this page composes more slices;
// five positional services would be five chances to swap two of them.
type Options struct {
	Reminders *meals.MealReminderService
	CheckIns  *checkins.Service
	Hydration *hydration.Service
	Sleep     *sleep.Service
	Habits    *habits.Service
}

func NewHandler(opts Options) *Handler {
	return &Handler{
		reminders: opts.Reminders,
		checkins:  opts.CheckIns,
		hydration: opts.Hydration,
		sleep:     opts.Sleep,
		habits:    opts.Habits,
	}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/care", h.show)

	r.Post("/care/reminders", h.createReminder)
	r.Post("/care/reminders/{id}/toggle", h.toggleReminder)
	r.Post("/care/reminders/{id}/delete", h.deleteReminder)

	r.Post("/care/water", h.logWater)
	r.Post("/care/water/{id}/undo", h.undoWater)

	r.Post("/care/sleep", h.logSleep)

	r.Post("/care/habits", h.createHabit)
	r.Post("/care/habits/{id}/toggle", h.toggleHabit)
	r.Post("/care/habits/{id}/delete", h.deleteHabit)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, carepages.Forms{})
}

// render rebuilds the whole page. Each section is fetched independently, so
// one slice failing takes the page down rather than silently showing a
// half-truth — unlike the coach, a person reading "0 drinks" needs to know
// whether that means none or unknown.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, forms carepages.Forms) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	due, err := h.reminders.DueNow(ctx, user.ID, time.Now().In(user.Location()))
	if err != nil {
		h.fail(w, r, err)
		return
	}

	allReminders, err := h.reminders.List(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	checkedInToday := true
	if _, err = h.checkins.Today(ctx, user); err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			checkedInToday = false
		} else {
			h.fail(w, r, err)
			return
		}
	}

	water, err := h.hydration.Today(ctx, user)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	waterEntries, err := h.hydration.TodayEntries(ctx, user)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	lastNight, sleptLastNight, err := h.sleep.Today(ctx, user)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	habitStats, err := h.habits.Today(ctx, user)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	data := carepages.Data{
		DueReminders:   due,
		AllReminders:   allReminders,
		CheckedInToday: checkedInToday,
		Water:          water,
		WaterEntries:   waterEntries,
		LastNight:      lastNight,
		SleptLastNight: sleptLastNight,
		Habits:         habitStats,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := carepages.Page(user, data, forms).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render care", slog.Any("error", err))
	}
}

// ---------------------------------------------------------------------------
// Reminders
// ---------------------------------------------------------------------------

func (h *Handler) createReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := carepages.ReminderForm{Label: r.PostFormValue("label"), TimeOfDay: r.PostFormValue("time_of_day")}
	form.DaysOfWeek = parseDays(r.Form["days_of_week"])

	if _, err := h.reminders.Create(r.Context(), user.ID, meals.ReminderInput{
		Label: form.Label, TimeOfDay: form.TimeOfDay, DaysOfWeek: form.DaysOfWeek,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.render(w, r, http.StatusUnprocessableEntity, carepages.Forms{Reminder: form})
			return
		}
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

func (h *Handler) toggleReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	enabled := r.PostFormValue("enabled") == "true"
	if _, err := h.reminders.Toggle(r.Context(), id, user.ID, enabled); err != nil {
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

func (h *Handler) deleteReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	if err := h.reminders.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

// ---------------------------------------------------------------------------
// Water
// ---------------------------------------------------------------------------

func (h *Handler) logWater(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	amount, err := strconv.Atoi(r.PostFormValue("amount_ml"))
	if err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	if _, err := h.hydration.Log(r.Context(), user, amount); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			h.render(w, r, http.StatusUnprocessableEntity, carepages.Forms{Water: carepages.WaterForm{Errors: fieldErrs.Messages()}})
			return
		}
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

func (h *Handler) undoWater(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	if err := h.hydration.Undo(r.Context(), user, id); err != nil {
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

// ---------------------------------------------------------------------------
// Sleep
// ---------------------------------------------------------------------------

func (h *Handler) logSleep(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := carepages.SleepForm{
		Hours:    r.PostFormValue("hours"),
		Minutes:  r.PostFormValue("minutes"),
		Quality:  r.PostFormValue("quality"),
		Bedtime:  r.PostFormValue("bedtime"),
		WakeTime: r.PostFormValue("wake_time"),
		Notes:    r.PostFormValue("notes"),
	}

	in := sleep.Input{
		DurationMinutes: atoiOrZero(form.Hours)*60 + atoiOrZero(form.Minutes),
		Bedtime:         form.Bedtime,
		WakeTime:        form.WakeTime,
		Notes:           form.Notes,
	}
	// Quality is optional, so an unset select stays nil rather than becoming
	// a zero the validator would then reject.
	if q := atoiOrZero(form.Quality); q > 0 {
		in.Quality = &q
	}

	if _, err := h.sleep.LogToday(r.Context(), user, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.render(w, r, http.StatusUnprocessableEntity, carepages.Forms{Sleep: form})
			return
		}
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

// ---------------------------------------------------------------------------
// Habits
// ---------------------------------------------------------------------------

func (h *Handler) createHabit(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := carepages.HabitForm{
		Name:       r.PostFormValue("name"),
		Domain:     r.PostFormValue("domain"),
		DaysOfWeek: parseDays(r.Form["days_of_week"]),
	}

	days := make([]time.Weekday, 0, len(form.DaysOfWeek))
	for _, d := range form.DaysOfWeek {
		days = append(days, time.Weekday(d))
	}

	if _, err := h.habits.Create(r.Context(), user, habits.Input{
		Name: form.Name, Domain: form.Domain, Days: days, Active: true,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.render(w, r, http.StatusUnprocessableEntity, carepages.Forms{Habit: form})
			return
		}
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

// toggleHabit ticks or unticks today, driven by the submitted state rather
// than by reading the current one, so a double submit is idempotent instead
// of flipping twice.
func (h *Handler) toggleHabit(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	var err error
	if r.PostFormValue("done") == "true" {
		err = h.habits.Complete(r.Context(), user, id)
	} else {
		err = h.habits.Uncomplete(r.Context(), user, id)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

func (h *Handler) deleteHabit(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	if err := h.habits.Delete(r.Context(), user, id); err != nil {
		h.fail(w, r, err)
		return
	}

	h.back(w, r)
}

// ---------------------------------------------------------------------------

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

// back returns to the page after a successful write. POST-redirect-GET, so a
// refresh does not log another glass of water.
func (h *Handler) back(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/app/care", http.StatusSeeOther)
}

func parseDays(raw []string) []int {
	out := make([]int, 0, len(raw))
	for _, s := range raw {
		if d, err := strconv.Atoi(s); err == nil {
			out = append(out, d)
		}
	}
	return out
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("care request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
