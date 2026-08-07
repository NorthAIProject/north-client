// Package care composes meals.MealReminderService and checkins.Service into
// one accountability page: what's due, what's not done yet, and managing the
// reminders themselves. It owns no repository and no ContextSource — the
// underlying facts either already reach the coach through another section
// (nutrition) or are UI-only ("your to-do list today"), and duplicating them
// under a second heading would be noise, not new information.
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
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	carepages "github.com/NorthAIProject/north-client/web/care"
)

type Handler struct {
	reminders *meals.MealReminderService
	checkins  *checkins.Service
}

func NewHandler(reminders *meals.MealReminderService, checkinSvc *checkins.Service) *Handler {
	return &Handler{reminders: reminders, checkins: checkinSvc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/care", h.show)
	r.Post("/care/reminders", h.createReminder)
	r.Post("/care/reminders/{id}/toggle", h.toggleReminder)
	r.Post("/care/reminders/{id}/delete", h.deleteReminder)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, carepages.ReminderForm{})
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, f carepages.ReminderForm) {
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
	if _, err := h.checkins.Today(ctx, user); err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			checkedInToday = false
		} else {
			h.fail(w, r, err)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := carepages.Page(user, due, checkedInToday, allReminders, f).Render(ctx, w); err != nil {
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
	for _, raw := range r.Form["days_of_week"] {
		if d, err := strconv.Atoi(raw); err == nil {
			form.DaysOfWeek = append(form.DaysOfWeek, d)
		}
	}

	if _, err := h.reminders.Create(r.Context(), user.ID, meals.ReminderInput{
		Label: form.Label, TimeOfDay: form.TimeOfDay, DaysOfWeek: form.DaysOfWeek,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.render(w, r, http.StatusUnprocessableEntity, form)
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/care", http.StatusSeeOther)
}

func (h *Handler) toggleReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
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

	http.Redirect(w, r, "/app/care", http.StatusSeeOther)
}

func (h *Handler) deleteReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.reminders.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/care", http.StatusSeeOther)
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
