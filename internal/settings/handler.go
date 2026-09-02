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

	"github.com/NorthAIProject/north-client/internal/account"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/integrations"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/push"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/toolaudit"
	"github.com/NorthAIProject/north-client/internal/users"
	settingspages "github.com/NorthAIProject/north-client/web/settings"
)

type Handler struct {
	users         *users.Service
	preferences   *preferences.Service
	notifications *notifications.Service
	diets         *meals.DietPreferenceService
	connections   *connections.Service
	aicreds       *aicreds.Service
	audit         *toolaudit.Service

	// messaging issues and revokes platform link codes. Nil is not a valid
	// state — the service is built whether or not a bot token exists, because
	// this page is where a link begins.
	messaging *messaging.Service

	// telegram is configuration, not a service: the page needs to know whether
	// a bot exists and what it is called, and neither is a decision.
	telegram config.TelegramConfig

	// integrations is North's outbound MCP connections. Nil leaves the calendar
	// card off, which is what a process with no encryption key gets.
	integrations *integrations.Service

	// account and mw are here rather than in a handler of their own because
	// deleting is refused by re-rendering this page with the reason on it, and
	// that render already lives here. A separate package would have to export
	// the whole of it to say one thing back.
	account *account.Service
	mw      *auth.Middleware

	// push is Web Push. Nil, or a service without keys, leaves the row off
	// the notifications card.
	push *push.Service
}

// WithIntegrations wires the calendar connection card.
//
// A setter rather than a tenth positional argument to NewHandler, which already
// takes nine.
func (h *Handler) WithIntegrations(svc *integrations.Service) *Handler {
	h.integrations = svc
	return h
}

// WithPush wires the "nudges on this device" row.
func (h *Handler) WithPush(svc *push.Service) *Handler {
	h.push = svc
	return h
}

func NewHandler(
	u *users.Service,
	p *preferences.Service,
	n *notifications.Service,
	d *meals.DietPreferenceService,
	c *connections.Service,
	a *aicreds.Service,
	audit *toolaudit.Service,
	m *messaging.Service,
	telegram config.TelegramConfig,
	acct *account.Service,
	mw *auth.Middleware,
) *Handler {
	return &Handler{
		users: u, preferences: p, notifications: n, diets: d, connections: c, aicreds: a,
		audit: audit, messaging: m, telegram: telegram, account: acct, mw: mw,
	}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/settings", h.show)
	r.Post("/settings/profile", h.updateProfile)
	r.Post("/settings/preferences", h.updatePreferences)
	r.Post("/settings/notifications", h.updateNotifications)
	r.Post("/settings/diets", h.updateDiets)

	r.Get("/settings/connections", h.showConnections)
	r.Post("/settings/connections", h.createConnection)
	r.Post("/settings/connections/{id}/revoke", h.revokeConnection)
	r.Get("/settings/connections/{id}/status", h.connectionStatus)

	r.Get("/settings/activity", h.showActivity)

	r.Post("/settings/messaging/telegram", h.createTelegramCode)
	r.Post("/settings/messaging/telegram/disconnect", h.disconnectTelegram)

	r.Post("/settings/integrations/calendar", h.connectCalendar)
	r.Post("/settings/integrations/calendar/disconnect", h.disconnectCalendar)

	r.Post("/settings/ai", h.updateAICredential)
	r.Post("/settings/ai/remove", h.removeAICredential)

	r.Post("/settings/account/delete", h.deleteAccount)
}

// deleteAccount ends the account and signs the browser out.
//
// The redirect has to leave /app: by the time it runs there is no user behind
// the session cookie, and every route under /app would bounce it to the login
// page anyway — with a message about being signed out, which is not what
// happened.
func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	if _, err := h.account.Delete(r.Context(), user, r.PostFormValue(account.ConfirmField)); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			h.render(w, r, settingspages.ProfileFormFor(user), nil, nil, "",
				fieldErrs.Messages()[account.ConfirmField])
			return
		}
		h.fail(w, r, err)
		return
	}

	h.mw.ClearCookie(w)
	http.Redirect(w, r, "/?deleted=1", http.StatusSeeOther)
}

// createTelegramCode issues a link code and renders it.
//
// Like createConnection, this cannot redirect: the code exists in plaintext
// only in this response, and a 303 would send the browser to a page with no way
// to learn what it was.
func (h *Handler) createTelegramCode(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	code, err := h.messaging.IssueCode(r.Context(), user.ID, messaging.PlatformTelegram)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.renderConnections(w, r, settingspages.ConnectForm{Kind: connections.ClientClaudeCode}, nil, nil, code, "")
}

func (h *Handler) disconnectTelegram(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if _, err := h.messaging.Unlink(r.Context(), user.ID, messaging.PlatformTelegram); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings/connections", http.StatusSeeOther)
}

// connectCalendar links an external MCP calendar server.
//
// Renders rather than redirects on failure: the endpoint and the reason it did
// not work are both worth showing next to the field somebody just filled in.
func (h *Handler) connectCalendar(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if h.integrations == nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	err := h.integrations.Connect(r.Context(),
		user.ID,
		r.PostFormValue("endpoint"),
		r.PostFormValue("token"),
	)
	switch {
	case err == nil:
		http.Redirect(w, r, "/app/settings/connections", http.StatusSeeOther)
	case apperr.Is(err, apperr.ErrValidation), apperr.Is(err, apperr.ErrUnavailable):
		h.renderConnections(w, r,
			settingspages.ConnectForm{Kind: connections.ClientClaudeCode}, nil, nil, "", err.Error())
	default:
		h.fail(w, r, err)
	}
}

func (h *Handler) disconnectCalendar(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if h.integrations == nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.integrations.Disconnect(r.Context(), user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/settings/connections", http.StatusSeeOther)
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
		BaseURL:  r.PostFormValue("base_url"),
	}

	if _, err := h.aicreds.Save(r.Context(), user.ID, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form := settingspages.ProviderFormFor(in)
			form.Errors = fieldErrs.Messages()
			h.renderConnections(w, r, settingspages.ConnectForm{Kind: connections.ClientClaudeCode}, nil, &form, "", "")
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
	h.render(w, r, settingspages.ProfileFormFor(auth.MustUser(r.Context())), nil, nil, "", "")
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
		CoachingTone:  users.Tone(strings.TrimSpace(r.PostFormValue("coaching_tone"))),
	}

	if _, err := h.users.UpdateProfile(r.Context(), user.ID, users.Profile{
		DisplayName: form.DisplayName, Timezone: form.Timezone,
		CoachingStyle: form.CoachingStyle, CoachingTone: form.CoachingTone,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.render(w, r, form, nil, nil, "", "")
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
			h.render(w, r, settingspages.ProfileFormFor(user), &prefsForm, nil, "", "")
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings", http.StatusSeeOther)
}

// updateNotifications saves what North may say unprompted.
func (h *Handler) updateNotifications(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	in := notifications.Input{
		NudgeMissedCheckIn: checked(r, "nudge_missed_checkin"),
		NudgeGoalDeadline:  checked(r, "nudge_goal_deadline"),
		CoachActivity:      checked(r, "coach_activity"),
		TrainingReminders:  checked(r, "training_reminders"),
		WeeklyReportAuto:   checked(r, "weekly_report_auto"),
		DailyBriefingAuto:  checked(r, "daily_briefing_auto"),
		QuietHoursEnabled:  checked(r, "quiet_hours_enabled"),
		QuietStart:         strings.TrimSpace(r.PostFormValue("quiet_start")),
		QuietEnd:           strings.TrimSpace(r.PostFormValue("quiet_end")),
	}

	every, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("photo_every_days")))
	remind, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("photo_reminder_days")))
	sched := notifications.ScheduleInput{
		Kind:         notifications.KindPhoto,
		Enabled:      checked(r, "photo_ask_enabled"),
		EveryDays:    every,
		ReminderDays: remind,
	}

	if _, err := h.notifications.Upsert(r.Context(), user.ID, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			notifForm := settingspages.NotificationsFormFrom(in)
			notifForm.PhotoAskEnabled = sched.Enabled
			notifForm.PhotoEveryDays = sched.EveryDays
			notifForm.PhotoReminderDays = sched.ReminderDays
			notifForm.Errors = fieldErrs.Messages()
			h.render(w, r, settingspages.ProfileFormFor(user), nil, &notifForm, "", "")
			return
		}
		h.fail(w, r, err)
		return
	}

	if _, err := h.notifications.UpsertSchedule(r.Context(), user.ID, sched); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			photo := notifications.Schedule{
				Enabled: sched.Enabled, EveryDays: sched.EveryDays, ReminderDays: sched.ReminderDays,
			}
			n := notifications.Prefs{
				NudgeMissedCheckIn: in.NudgeMissedCheckIn,
				NudgeGoalDeadline:  in.NudgeGoalDeadline,
				CoachActivity:      in.CoachActivity,
				TrainingReminders:  in.TrainingReminders,
				WeeklyReportAuto:   in.WeeklyReportAuto,
				DailyBriefingAuto:  in.DailyBriefingAuto,
				QuietHoursEnabled:  in.QuietHoursEnabled,
				QuietStart:         in.QuietStart,
				QuietEnd:           in.QuietEnd,
			}
			notifForm := settingspages.NotificationsFormFor(n, photo)
			notifForm.Errors = fieldErrs.Messages()
			h.render(w, r, settingspages.ProfileFormFor(user), nil, &notifForm, "", "")
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/settings", http.StatusSeeOther)
}

// checked reads a checkbox. An unchecked box sends nothing at all, which is
// what makes this a helper rather than a PostFormValue comparison at each of
// the four call sites above.
func checked(r *http.Request, name string) bool {
	return r.PostFormValue(name) != ""
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

// showActivity lists what North has done on this person's behalf.
func (h *Handler) showActivity(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	executions, err := h.audit.List(r.Context(), user.ID, 0)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingspages.ActivityPage(user, executions).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render activity", slog.Any("error", err))
	}
}

func (h *Handler) showConnections(w http.ResponseWriter, r *http.Request) {
	h.renderConnections(w, r, settingspages.ConnectForm{Kind: connections.ClientClaudeCode}, nil, nil, "", "")
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
			h.renderConnections(w, r, form, nil, nil, "", "")
			return
		}
		h.fail(w, r, err)
		return
	}

	h.renderConnections(w, r, settingspages.ConnectForm{Kind: form.Kind}, &issued, nil, "", "")
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
// telegramCode is non-empty only on the response that issued one, for the same
// reason issued is: it is not stored in a form a page could read back.
func (h *Handler) renderConnections(
	w http.ResponseWriter,
	r *http.Request,
	form settingspages.ConnectForm,
	issued *connections.Issued,
	providerForm *settingspages.ProviderForm,
	telegramCode string,
	calendarErr string,
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

	// One preview per client, so the page can show what setup involves before
	// anybody creates a credential to find out. Built here rather than in the
	// template because Instructions belongs to the service, and a template that
	// reached for it would be the first one in this codebase to do so.
	previews := make([]connections.Setup, 0, len(connections.ClientKinds))
	for _, kind := range connections.ClientKinds {
		previews = append(previews, h.connections.Preview(kind))
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
				provider.Form = settingspages.ProviderFormFor(aicreds.Input{
					Provider: cred.Provider, Model: cred.Model, BaseURL: cred.BaseURL,
				})
			}
		case apperr.Is(err, apperr.ErrNotFound):
		default:
			h.fail(w, r, err)
			return
		}
	}

	// The person's own calendar, reached over MCP. Absent is the normal state.
	var calendar settingspages.CalendarPanel
	if h.integrations != nil {
		calendar.Enabled = true
		conn, ok, err := h.integrations.Status(ctx, user.ID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		if ok {
			calendar.Connected = &conn
		}
	}
	calendar.Error = calendarErr

	telegram := settingspages.TelegramPanel{
		Enabled:     h.telegram.Enabled(),
		BotUsername: h.telegram.BotUsername,
		Code:        telegramCode,
	}

	// Only asked when there is a bot to link to. A deployment without one
	// renders no card, so the query would be answering a question nobody asked.
	if telegram.Enabled {
		links, err := h.messaging.Links(ctx, user.ID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		for i, link := range links {
			if link.Platform == messaging.PlatformTelegram {
				telegram.Linked = &links[i]
				break
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingspages.ConnectionsPage(user, list, form, issued, setup, previews, provider, telegram, calendar).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render connections", slog.Any("error", err))
	}
}

// render loads whatever the caller didn't already have (preferences,
// notification prefs, diets) and shows the page. A nil form is one the caller
// had no reason to build — on GET /settings, or when some other form on the
// page is the one that failed validation.
//
// deleteErr is the account-deletion refusal, which has no form of its own.
func (h *Handler) render(
	w http.ResponseWriter,
	r *http.Request,
	profileForm settingspages.ProfileForm,
	prefsForm *settingspages.PreferencesForm,
	notifForm *settingspages.NotificationsForm,
	saved string,
	deleteErr string,
) {
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

	if notifForm == nil {
		n, err := h.notifications.Get(ctx, user.ID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		photo, photoErr := h.notifications.PhotoSchedule(ctx, user.ID)
		if photoErr != nil {
			h.fail(w, r, photoErr)
			return
		}
		f := settingspages.NotificationsFormFor(n, photo)
		notifForm = &f
	}

	// Filled in on every render, including a rejected submission: the push row
	// is not part of the form, so it must not vanish when the form is wrong.
	if h.push != nil && h.push.Enabled() {
		devices, err := h.push.Count(ctx, user.ID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		notifForm.PushEnabled = true
		notifForm.PushPublicKey = h.push.PublicKey()
		notifForm.PushDevices = devices
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
	if err := settingspages.Page(user, summary, profileForm, *prefsForm, *notifForm, allDiets, selected, saved, deleteErr).Render(ctx, w); err != nil {
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
