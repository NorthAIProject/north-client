// Package care composes the daily accountability page: what's due, what's not
// done yet, and the quick logging that belongs to a person's day rather than
// to a training session — water, sleep and habits, alongside meal reminders
// and check-in status.
package care

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/htmx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/sleep"
	carepages "github.com/NorthAIProject/north-client/web/care"
)

type Handler struct {
	svc *Service
}

func NewHandler(opts Options) *Handler {
	return &Handler{svc: NewService(opts)}
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
	h.respond(w, r, http.StatusOK, carepages.Forms{})
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, status int, forms carepages.Forms) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	snap, err := h.svc.Load(ctx, user)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	inst, err := buildView(snap)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	data := pageData(snap, inst)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	var component templ.Component
	if htmx.IsRequest(r) {
		component = carepages.Panel(user, data, forms)
	} else {
		component = carepages.Page(user, data, forms)
	}
	if err := component.Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render care", slog.Any("error", err))
	}
}

func (h *Handler) createReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := carepages.ReminderForm{Label: r.PostFormValue("label"), TimeOfDay: r.PostFormValue("time_of_day")}
	form.DaysOfWeek = parseDays(r.Form["days_of_week"])

	if _, err := h.svc.reminders.Create(r.Context(), user.ID, meals.ReminderInput{
		Label: form.Label, TimeOfDay: form.TimeOfDay, DaysOfWeek: form.DaysOfWeek,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.respond(w, r, http.StatusUnprocessableEntity, carepages.Forms{Reminder: form})
			return
		}
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
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
	if _, err := h.svc.reminders.Toggle(r.Context(), id, user.ID, enabled); err != nil {
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
}

func (h *Handler) deleteReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	if err := h.svc.reminders.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
}

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

	if _, err := h.svc.hydration.Log(r.Context(), user, amount); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			h.respond(w, r, http.StatusUnprocessableEntity, carepages.Forms{Water: carepages.WaterForm{Errors: fieldErrs.Messages()}})
			return
		}
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
}

func (h *Handler) undoWater(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	if err := h.svc.hydration.Undo(r.Context(), user, id); err != nil {
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
}

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
	if q := atoiOrZero(form.Quality); q > 0 {
		in.Quality = &q
	}

	if _, err := h.svc.sleep.LogToday(r.Context(), user, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.respond(w, r, http.StatusUnprocessableEntity, carepages.Forms{Sleep: form})
			return
		}
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
}

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

	if _, err := h.svc.habits.Create(r.Context(), user, habits.Input{
		Name: form.Name, Domain: form.Domain, Days: days, Active: true,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.respond(w, r, http.StatusUnprocessableEntity, carepages.Forms{Habit: form})
			return
		}
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
}

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
		err = h.svc.habits.Complete(r.Context(), user, id)
	} else {
		err = h.svc.habits.Uncomplete(r.Context(), user, id)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
}

func (h *Handler) deleteHabit(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, ok := h.pathID(w, r)
	if !ok {
		return
	}

	if err := h.svc.habits.Delete(r.Context(), user, id); err != nil {
		h.fail(w, r, err)
		return
	}

	h.done(w, r, carepages.Forms{})
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) done(w http.ResponseWriter, r *http.Request, forms carepages.Forms) {
	if htmx.IsRequest(r) {
		h.respond(w, r, http.StatusOK, forms)
		return
	}
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
