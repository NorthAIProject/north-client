// Package settings composes users.Service, preferences.Service,
// meals.DietPreferenceService, and connections.Service into the account
// settings pages. It owns no repository of its own — every field on these
// pages already has a home in one of those services.
package settings

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/connections"
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
	connections *connections.Service
	aicreds     *aicreds.Service
}

func NewHandler(u *users.Service, p *preferences.Service, d *meals.DietPreferenceService, c *connections.Service, a *aicreds.Service) *Handler {
	return &Handler{users: u, preferences: p, diets: d, connections: c, aicreds: a}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/settings", h.show)
	r.Post("/settings/profile", h.updateProfile)
	r.Post("/settings/preferences", h.updatePreferences)
	r.Post("/settings/diets", h.updateDiets)

	r.Get("/settings/connections", h.showConnections)
	r.Post("/settings/connections", h.createConnection)
	r.Post("/settings/connections/{id}/revoke", h.revokeConnection)
	r.Get("/settings/connections/{id}/status", h.connectionStatus)

	r.Post("/settings/ai", h.updateAICredential)
	r.Post("/settings/ai/remove", h.removeAICredential)
}

func (h *Handler) updateAICredential(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	in := aicreds.Input{
		Provider: r.PostFormValue("provider"),
		APIKey:   r.PostFormValue("api_key"),
		Model:    r.PostFormValue("model"),
	}

	if _, err := h.aicreds.Save(r.Context(), user.ID, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form := settingspages.ProviderFormFor(in)
			form.Errors = fieldErrs.Messages()
			h.renderConnections(w, r, settingspages.ConnectForm{Kind: connections.ClientClaudeCode}, nil, &form)
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings/connections", http.StatusSeeOther)
}

func (h *Handler) removeAICredential(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	// Already gone is the outcome the caller wanted, so it is not an error.
	if err := h.aicreds.Delete(r.Context(), user.ID); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings/connections", http.StatusSeeOther)
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

func (h *Handler) showConnections(w http.ResponseWriter, r *http.Request) {
	h.renderConnections(w, r, settingspages.ConnectForm{Kind: connections.ClientClaudeCode}, nil, nil)
}

// createConnection issues a token and renders it.
//
// The only handler on this page that does not redirect, and it cannot: the
// plaintext token exists once, in this response. A 303 would send the browser
// to a page that has no way to learn what the token was.
func (h *Handler) createConnection(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := settingspages.ConnectForm{
		Name: strings.TrimSpace(r.PostFormValue("name")),
		Kind: connections.ClientKind(r.PostFormValue("client_kind")),
	}

	issued, err := h.connections.Issue(r.Context(), user.ID, form.Name, form.Kind)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.renderConnections(w, r, form, nil, nil)
			return
		}
		h.fail(w, r, err)
		return
	}

	h.renderConnections(w, r, settingspages.ConnectForm{Kind: form.Kind}, &issued, nil)
}

// connectionStatus answers the poll behind "waiting for first use".
//
// A fragment rather than a JSON endpoint, because the answer is markup that
// decides whether to ask again — the poll stops by returning something with no
// hx-trigger on it. A revoked connection is gone from the list and answers 404,
// which also stops the poll.
func (h *Handler) connectionStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	conn, err := h.connections.Get(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// Unparseable means zero, which only costs the caller a longer poll.
	tries, _ := strconv.Atoi(r.URL.Query().Get("tries"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingspages.ConnectionStatus(conn, tries).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render connection status", slog.Any("error", err))
	}
}

func (h *Handler) revokeConnection(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.connections.Revoke(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings/connections", http.StatusSeeOther)
}

// renderConnections shows the connections page.
//
// issued is non-nil only on the response that created a token; the setup
// instructions are built here rather than stored, because they carry it.
// providerForm is non-nil only when a provider submission failed validation
// and has to come back with its messages.
func (h *Handler) renderConnections(
	w http.ResponseWriter,
	r *http.Request,
	form settingspages.ConnectForm,
	issued *connections.Issued,
	providerForm *settingspages.ProviderForm,
) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	list, err := h.connections.List(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	var setup connections.Setup
	if issued != nil {
		setup = h.connections.Instructions(issued.Kind, issued.Token)
	}

	provider := settingspages.ProviderPanel{
		Enabled: h.aicreds.Enabled(),
		Catalog: h.aicreds.Providers(),
		Form:    settingspages.ProviderForm{Provider: "openrouter"},
	}
	if providerForm != nil {
		provider.Form = *providerForm
	}

	// A stored credential is what decides whether the card opens on "use my
	// own key". Absent is the normal case and not an error.
	if h.aicreds.Enabled() {
		cred, err := h.aicreds.Get(ctx, user.ID)
		switch {
		case err == nil:
			provider.Current = &cred
			if providerForm == nil {
				provider.Form = settingspages.ProviderFormFor(aicreds.Input{Provider: cred.Provider, Model: cred.Model})
			}
		case apperr.Is(err, apperr.ErrNotFound):
		default:
			h.fail(w, r, err)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingspages.ConnectionsPage(user, list, form, issued, setup, provider).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render connections", slog.Any("error", err))
	}
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

	conns, err := h.connections.List(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	summary := buildSettingsSummary(user, prefsForm.UnitsSystem, len(userDiets), len(conns))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingspages.Page(user, summary, profileForm, *prefsForm, allDiets, selected, saved).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render settings", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	case apperr.Is(err, apperr.ErrUnavailable):
		// A feature this deployment cannot offer — storing a provider key with
		// no encryption configured. Nothing is wrong with the request, so a
		// 500 would send the caller looking for a fault that is not there.
		http.Error(w, "That is not available on this server.", http.StatusServiceUnavailable)
	default:
		middleware.FromContext(r.Context()).Error("settings request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
