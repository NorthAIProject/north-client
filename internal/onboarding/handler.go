package onboarding

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	onboardingpages "github.com/NorthAIProject/north-client/web/onboarding"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts first-run onboarding. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/onboarding", h.show)
	r.Post("/onboarding", h.complete)
	r.Post("/onboarding/skip", h.skip)
	r.Get("/onboarding/done", h.done)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if !user.NeedsOnboarding() {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, onboardingpages.FormPage(onboardingpages.Form{}))
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if !user.NeedsOnboarding() {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)
	answers, err := ValidateAnswers(form.FocusAreas, form.StylePreset, form.StyleCustom, form.GoalTitle)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			render(w, r, http.StatusUnprocessableEntity, onboardingpages.FormPage(form))
			return
		}
		h.fail(w, r, err)
		return
	}

	if _, err := h.svc.Complete(r.Context(), user, answers); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/onboarding/done", http.StatusSeeOther)
}

func (h *Handler) skip(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if !user.NeedsOnboarding() {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}

	if _, err := h.svc.Skip(r.Context(), user); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (h *Handler) done(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if user.NeedsOnboarding() {
		http.Redirect(w, r, "/app/onboarding", http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, onboardingpages.DonePage(user))
}

func formFrom(r *http.Request) onboardingpages.Form {
	return onboardingpages.Form{
		FocusAreas:  r.Form["focus_areas"],
		StylePreset: r.PostFormValue("coaching_style_preset"),
		StyleCustom: r.PostFormValue("coaching_style_custom"),
		GoalTitle:   r.PostFormValue("goal_title"),
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("onboarding request failed", slog.Any("error", err))
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
