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
	"github.com/NorthAIProject/north-client/internal/users"
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

	form := checkinpages.CheckInForm{Mood: defaultScale, Energy: defaultScale}
	if today, err := h.svc.Today(r.Context(), user); err == nil {
		form = checkinpages.FormFor(today)
	}

	h.renderForm(w, r, user, form, http.StatusOK)
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)
	in := inputFrom(form)

	saved, err := h.svc.UpsertToday(r.Context(), user, in)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.renderForm(w, r, user, form, http.StatusUnprocessableEntity)
			return
		}
		h.fail(w, r, err)
		return
	}

	h.renderSaved(w, r, user, saved)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)
	in := inputFrom(form)

	saved, err := h.svc.Update(r.Context(), id, user.ID, in)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.renderForm(w, r, user, form, http.StatusUnprocessableEntity)
			return
		}
		h.fail(w, r, err)
		return
	}

	h.renderSaved(w, r, user, saved)
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

// defaultScale is where the mood and energy pickers start on a blank check-in:
// the middle of the 1–5 range, so the guided flow never opens on an unanswerable
// question and a user who taps straight through still submits something valid.
const defaultScale = 3

// isHTMX reports whether the request came from htmx rather than a plain form
// post. The no-JavaScript path has to keep working, so every response that htmx
// would swap has a full-page counterpart.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// renderForm draws the guided form, either as the swappable panel for htmx or
// as the whole page for a plain request.
//
// Both the first view and a rejected submission come through here so the two
// cannot drift: a validation failure has to return the user to the same form
// with their answers intact, and previously it rebuilt the page by hand and
// reported success while doing it.
func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, user users.User, form checkinpages.CheckInForm, status int) {
	list, err := h.svc.List(r.Context(), user.ID, listDefault)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	active, _ := h.goals.ListActive(r.Context(), user.ID)

	chartList, err := h.svc.RecentForContext(r.Context(), user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	inst, err := buildInstruments(user, chartList)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	if isHTMX(r) {
		render(w, r, status, checkinpages.Panel(list, form, active))
		return
	}

	streak, _ := h.svc.Streak(r.Context(), user)
	// Only the post-redirect landing carries saved=1. A rejected POST has no
	// query string, so the confirmation cannot appear above an error.
	saved := r.URL.Query().Get("saved") == "1"
	render(w, r, status, checkinpages.IndexPage(user, list, form, active, streak, saved, inst))
}

// renderSaved confirms a stored check-in in place for htmx, and falls back to
// the post-redirect-get for a plain form post.
func (h *Handler) renderSaved(w http.ResponseWriter, r *http.Request, user users.User, saved CheckIn) {
	if !isHTMX(r) {
		http.Redirect(w, r, "/app/check-ins?saved=1", http.StatusSeeOther)
		return
	}

	list, err := h.svc.List(r.Context(), user.ID, listDefault)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	streak, _ := h.svc.Streak(r.Context(), user)

	render(w, r, http.StatusOK, checkinpages.SavedPanel(saved, list, streak))
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
