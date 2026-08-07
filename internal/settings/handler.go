// Package settings composes users.Service, preferences.Service, and
// meals.DietPreferenceService into one account settings page. It owns no
// repository of its own — every field on the page already has a home in one
// of those three services.
package settings

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/preferences"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	settingspages "github.com/NorthAIProject/north-client/web/settings"
)

type Handler struct {
	users       *users.Service
	preferences *preferences.Service
	diets       *meals.DietPreferenceService
}

func NewHandler(u *users.Service, p *preferences.Service, d *meals.DietPreferenceService) *Handler {
	return &Handler{users: u, preferences: p, diets: d}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/settings", h.show)
	r.Post("/settings/profile", h.updateProfile)
	r.Post("/settings/preferences", h.updatePreferences)
	r.Post("/settings/diets", h.updateDiets)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, settingspages.ProfileFormFor(auth.MustUser(r.Context())), nil, "")
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := settingspages.ProfileForm{
		DisplayName:   strings.TrimSpace(r.PostFormValue("display_name")),
		Timezone:      strings.TrimSpace(r.PostFormValue("timezone")),
		CoachingStyle: strings.TrimSpace(r.PostFormValue("coaching_style")),
	}

	if _, err := h.users.UpdateProfile(r.Context(), user.ID, users.Profile{
		DisplayName: form.DisplayName, Timezone: form.Timezone, CoachingStyle: form.CoachingStyle,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.render(w, r, form, nil, "")
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings", http.StatusSeeOther)
}

func (h *Handler) updatePreferences(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	in := preferences.Input{
		UnitsSystem:       r.PostFormValue("units_system"),
		DefaultGoal:       r.PostFormValue("default_goal"),
		DefaultMacroSplit: r.PostFormValue("default_macro_split"),
	}

	if _, err := h.preferences.Upsert(r.Context(), user.ID, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			prefsForm := settingspages.PreferencesForm{
				UnitsSystem: in.UnitsSystem, DefaultGoal: in.DefaultGoal, DefaultMacroSplit: in.DefaultMacroSplit,
				Errors: fieldErrs.Messages(),
			}
			h.render(w, r, settingspages.ProfileFormFor(user), &prefsForm, "")
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings", http.StatusSeeOther)
}

func (h *Handler) updateDiets(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	var dietIDs []uuid.UUID
	for _, raw := range r.Form["diet_ids"] {
		id, err := uuid.Parse(raw)
		if err == nil {
			dietIDs = append(dietIDs, id)
		}
	}

	if err := h.diets.SetUserDiets(r.Context(), user.ID, dietIDs); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings", http.StatusSeeOther)
}

// render loads whatever the caller didn't already have (preferences, diets)
// and shows the page. prefsForm is nil on GET /settings and on a profile
// validation failure, where the caller has no reason to have built one.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, profileForm settingspages.ProfileForm, prefsForm *settingspages.PreferencesForm, saved string) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	if prefsForm == nil {
		p, err := h.preferences.Get(ctx, user.ID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		f := settingspages.PreferencesFormFor(p)
		prefsForm = &f
	}

	allDiets, err := h.diets.ListDiets(ctx)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	userDiets, err := h.diets.UserDiets(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	selected := make(map[uuid.UUID]bool, len(userDiets))
	for _, d := range userDiets {
		selected[d.ID] = true
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingspages.Page(user, profileForm, *prefsForm, allDiets, selected, saved).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render settings", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("settings request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
