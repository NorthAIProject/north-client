package checkins

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/goals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	checkinpages "github.com/NorthAIProject/north-client/web/checkins"
)

type Handler struct {
	svc   *Service
	goals *goals.Service
}

func NewHandler(svc *Service, goals *goals.Service) *Handler {
	return &Handler{svc: svc, goals: goals}
}

// Routes mounts check-in endpoints. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/check-ins", h.index)
	r.Post("/check-ins", h.upsert)
	r.Patch("/check-ins/{id}", h.update)
	r.Delete("/check-ins/{id}", h.delete)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	list, err := h.svc.List(r.Context(), user.ID, 30)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	form := checkinpages.CheckInForm{Mood: 3, Energy: 3}
	if today, err := h.svc.Today(r.Context(), user); err == nil {
		form = checkinpages.FormFor(today)
	}

	active, _ := h.goals.ListActive(r.Context(), user.ID)
	streak, _ := h.svc.Streak(r.Context(), user)
	saved := r.URL.Query().Get("saved") == "1"

	render(w, r, http.StatusOK, checkinpages.IndexPage(user, list, form, active, streak, saved))
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)
	in := inputFrom(form)

	if _, err := h.svc.UpsertToday(r.Context(), user, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			list, listErr := h.svc.List(r.Context(), user.ID, 30)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			active, _ := h.goals.ListActive(r.Context(), user.ID)
			streak, _ := h.svc.Streak(r.Context(), user)
			render(w, r, http.StatusUnprocessableEntity, checkinpages.IndexPage(user, list, form, active, streak, true))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/check-ins?saved=1", http.StatusSeeOther)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)
	in := inputFrom(form)

	if _, err := h.svc.Update(r.Context(), id, user.ID, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			list, listErr := h.svc.List(r.Context(), user.ID, 30)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			active, _ := h.goals.ListActive(r.Context(), user.ID)
			streak, _ := h.svc.Streak(r.Context(), user)
			render(w, r, http.StatusUnprocessableEntity, checkinpages.IndexPage(user, list, form, active, streak, true))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/check-ins?saved=1", http.StatusSeeOther)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	if err := h.svc.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/check-ins", http.StatusSeeOther)
}

func formFrom(r *http.Request) checkinpages.CheckInForm {
	f := checkinpages.CheckInForm{
		Wins:       strings.TrimSpace(r.PostFormValue("wins")),
		Challenges: strings.TrimSpace(r.PostFormValue("challenges")),
		Notes:      strings.TrimSpace(r.PostFormValue("notes")),
		GoalID:     strings.TrimSpace(r.PostFormValue("related_goal_id")),
	}
	if n, err := strconv.Atoi(r.PostFormValue("mood")); err == nil {
		f.Mood = n
	}
	if n, err := strconv.Atoi(r.PostFormValue("energy")); err == nil {
		f.Energy = n
	}
	return f
}

func inputFrom(f checkinpages.CheckInForm) Input {
	in := Input{
		Mood:       f.Mood,
		Energy:     f.Energy,
		Wins:       f.Wins,
		Challenges: f.Challenges,
		Notes:      f.Notes,
	}
	if f.GoalID != "" {
		if id, err := uuid.Parse(f.GoalID); err == nil {
			in.RelatedGoalID = &id
		}
	}
	return in
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("check-in request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}
