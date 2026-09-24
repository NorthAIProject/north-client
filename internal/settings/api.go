package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/account"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/preferences"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/users"
)

// API is Settings for native clients: the same services as the web page,
// one resource per card. It reuses the Handler's dependencies rather than
// taking its own, so the two cannot drift apart on what they are wired to.
//
// Secrets leave the server once or never. A provider key is write-only and
// comes back as a hint; an MCP token and a Telegram link code appear only in
// the response that created them.
type API struct {
	h *Handler
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(h *Handler) *API {
	return &API{h: h}
}

func (a *API) Routes(r chi.Router) {
	r.Get("/settings/profile", a.getProfile)
	r.Put("/settings/profile", a.putProfile)
	r.Get("/settings/preferences", a.getPreferences)
	r.Put("/settings/preferences", a.putPreferences)
	r.Get("/settings/notifications", a.getNotifications)
	r.Put("/settings/notifications", a.putNotifications)

	r.Get("/settings/ai", a.getAI)
	r.Put("/settings/ai", a.putAI)
	r.Delete("/settings/ai", a.deleteAI)

	r.Get("/settings/connections", a.listConnections)
	r.Post("/settings/connections", a.createConnection)
	r.Delete("/settings/connections/{connectionID}", a.revokeConnection)
	r.Get("/settings/activity", a.activity)

	r.Get("/settings/telegram", a.getTelegram)
	r.Post("/settings/telegram/code", a.telegramCode)
	r.Delete("/settings/telegram", a.unlinkTelegram)

	r.Get("/settings/calendar", a.getCalendar)
	r.Put("/settings/calendar", a.putCalendar)
	r.Delete("/settings/calendar", a.deleteCalendar)

	r.Post("/account/delete", a.deleteAccount)
}

// MARK: Profile

type Profile struct {
	Email         string `json:"email"`
	DisplayName   string `json:"displayName"`
	Timezone      string `json:"timezone"`
	Locale        string `json:"locale"`
	CoachingStyle string `json:"coachingStyle"`
	// CoachingTone is direct, warm, analytical or tough_love.
	CoachingTone string `json:"coachingTone"`
}

func projectProfile(u users.User) Profile {
	return Profile{
		Email: u.Email, DisplayName: u.DisplayName, Timezone: u.Timezone, Locale: string(u.Locale),
		CoachingStyle: u.CoachingStyle, CoachingTone: string(u.CoachingTone),
	}
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, projectProfile(auth.MustUser(r.Context())))
}

func (a *API) putProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	var req Profile
	if !read(w, r, &req) {
		return
	}
	updated, err := a.h.users.UpdateProfile(r.Context(), user.ID, users.Profile{
		DisplayName:   strings.TrimSpace(req.DisplayName),
		Timezone:      strings.TrimSpace(req.Timezone),
		Locale:        users.ResolveLocale(req.Locale),
		CoachingStyle: strings.TrimSpace(req.CoachingStyle),
		CoachingTone:  users.Tone(strings.TrimSpace(req.CoachingTone)),
	})
	if err != nil {
		httpx.Error(w, err, "Your profile could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectProfile(updated))
}

// MARK: Preferences

type Preferences struct {
	// UnitsSystem is metric or imperial.
	UnitsSystem       string `json:"unitsSystem"`
	DefaultGoal       string `json:"defaultGoal"`
	DefaultMacroSplit string `json:"defaultMacroSplit"`
}

func (a *API) getPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := a.h.preferences.Get(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Your preferences could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Preferences{UnitsSystem: p.UnitsSystem, DefaultGoal: p.DefaultGoal, DefaultMacroSplit: p.DefaultMacroSplit})
}

func (a *API) putPreferences(w http.ResponseWriter, r *http.Request) {
	var req Preferences
	if !read(w, r, &req) {
		return
	}
	p, err := a.h.preferences.Upsert(r.Context(), auth.MustUser(r.Context()).ID, preferences.Input{
		UnitsSystem: req.UnitsSystem, DefaultGoal: req.DefaultGoal, DefaultMacroSplit: req.DefaultMacroSplit,
	})
	if err != nil {
		httpx.Error(w, err, "Your preferences could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Preferences{UnitsSystem: p.UnitsSystem, DefaultGoal: p.DefaultGoal, DefaultMacroSplit: p.DefaultMacroSplit})
}

// MARK: Notifications

// Notifications is what North may say unprompted, and when not to.
type Notifications struct {
	NudgeMissedCheckIn bool `json:"nudgeMissedCheckIn"`
	NudgeGoalDeadline  bool `json:"nudgeGoalDeadline"`
	CoachActivity      bool `json:"coachActivity"`
	TrainingReminders  bool `json:"trainingReminders"`
	WeeklyReportAuto   bool `json:"weeklyReportAuto"`
	DailyBriefingAuto  bool `json:"dailyBriefingAuto"`
	// StatsDigestCadence is off, daily, weekly or monthly.
	StatsDigestCadence string `json:"statsDigestCadence"`
	QuietHoursEnabled  bool   `json:"quietHoursEnabled"`
	// QuietStart and QuietEnd are "HH:MM" in the person's time zone.
	QuietStart string `json:"quietStart"`
	QuietEnd   string `json:"quietEnd"`
	// Progress photos: whether to ask, how often, and how many days to wait
	// before a reminder.
	PhotoAskEnabled   bool `json:"photoAskEnabled"`
	PhotoEveryDays    int  `json:"photoEveryDays"`
	PhotoReminderDays int  `json:"photoReminderDays"`
}

func (a *API) getNotifications(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	n, err := a.h.notifications.Get(r.Context(), user.ID)
	if err != nil {
		httpx.Error(w, err, "Your notification settings could not be loaded.")
		return
	}
	photo, err := a.h.notifications.PhotoSchedule(r.Context(), user.ID)
	if err != nil {
		httpx.Error(w, err, "Your notification settings could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectNotifications(n, photo))
}

func (a *API) putNotifications(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	var req Notifications
	if !read(w, r, &req) {
		return
	}
	n, err := a.h.notifications.Upsert(r.Context(), user.ID, notifications.Input{
		NudgeMissedCheckIn: req.NudgeMissedCheckIn, NudgeGoalDeadline: req.NudgeGoalDeadline,
		CoachActivity: req.CoachActivity, TrainingReminders: req.TrainingReminders,
		WeeklyReportAuto: req.WeeklyReportAuto, DailyBriefingAuto: req.DailyBriefingAuto,
		StatsDigestCadence: req.StatsDigestCadence,
		QuietHoursEnabled:  req.QuietHoursEnabled, QuietStart: strings.TrimSpace(req.QuietStart), QuietEnd: strings.TrimSpace(req.QuietEnd),
	})
	if err != nil {
		httpx.Error(w, err, "Your notification settings could not be saved.")
		return
	}
	photo, err := a.h.notifications.UpsertSchedule(r.Context(), user.ID, notifications.ScheduleInput{
		Kind: notifications.KindPhoto, Enabled: req.PhotoAskEnabled, EveryDays: req.PhotoEveryDays, ReminderDays: req.PhotoReminderDays,
	})
	if err != nil {
		httpx.Error(w, err, "Your photo reminders could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectNotifications(n, photo))
}

func projectNotifications(n notifications.Prefs, photo notifications.Schedule) Notifications {
	return Notifications{
		NudgeMissedCheckIn: n.NudgeMissedCheckIn, NudgeGoalDeadline: n.NudgeGoalDeadline,
		CoachActivity: n.CoachActivity, TrainingReminders: n.TrainingReminders,
		WeeklyReportAuto: n.WeeklyReportAuto, DailyBriefingAuto: n.DailyBriefingAuto,
		StatsDigestCadence: n.StatsDigestCadence,
		QuietHoursEnabled:  n.QuietHoursEnabled, QuietStart: n.QuietStart, QuietEnd: n.QuietEnd,
		PhotoAskEnabled: photo.Enabled, PhotoEveryDays: photo.EveryDays, PhotoReminderDays: photo.ReminderDays,
	}
}

// MARK: Own AI provider

// AISettings is the bring-your-own-key card: which providers can be used, and
// the one in use, if any. The key itself is never returned.
type AISettings struct {
	// Enabled is false on a server with no encryption key, which cannot store
	// a credential.
	Enabled   bool         `json:"enabled"`
	Providers []AIProvider `json:"providers"`
	Current   *AICurrent   `json:"current,omitempty"`
}

type AIProvider struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	BaseURL      string `json:"baseUrl,omitempty"`
	DefaultModel string `json:"defaultModel,omitempty"`
	KeyHint      string `json:"keyHint,omitempty"`
}

type AICurrent struct {
	Provider string `json:"provider"`
	// KeyHint is the last characters of the stored key, for recognising it.
	KeyHint string `json:"keyHint"`
	Model   string `json:"model,omitempty"`
	BaseURL string `json:"baseUrl,omitempty"`
	// LastError is why the provider last refused, in words for the person.
	LastError   string     `json:"lastError,omitempty"`
	LastErrorAt *time.Time `json:"lastErrorAt,omitempty"`
	// SupportsTools is unknown until the background probe has run.
	SupportsTools *bool     `json:"supportsTools,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type AIRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
	Model    string `json:"model"`
	BaseURL  string `json:"baseUrl"`
}

func (a *API) getAI(w http.ResponseWriter, r *http.Request) {
	out, err := a.aiSettings(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Your AI provider could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) putAI(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	var req AIRequest
	if !read(w, r, &req) {
		return
	}
	if _, err := a.h.aicreds.Save(r.Context(), user.ID, aicreds.Input{
		Provider: req.Provider, APIKey: req.APIKey, Model: req.Model, BaseURL: req.BaseURL,
	}); err != nil {
		httpx.Error(w, err, "That provider could not be saved.")
		return
	}
	// As on the web: find out in the background whether this provider will
	// call the coach's tools. The answer appears on a later GET.
	go func(userID uuid.UUID) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), probeBudget)
		defer cancel()
		a.h.aicreds.ProbeTools(ctx, userID)
	}(user.ID)

	out, err := a.aiSettings(r.Context(), user.ID)
	if err != nil {
		httpx.Error(w, err, "Your AI provider could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteAI(w http.ResponseWriter, r *http.Request) {
	// Already gone is the outcome the caller wanted.
	if err := a.h.aicreds.Delete(r.Context(), auth.MustUser(r.Context()).ID); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		httpx.Error(w, err, "Your AI provider could not be removed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) aiSettings(ctx context.Context, userID uuid.UUID) (AISettings, error) {
	out := AISettings{Enabled: a.h.aicreds.Enabled(), Providers: []AIProvider{}}
	for _, p := range a.h.aicreds.Providers() {
		out.Providers = append(out.Providers, AIProvider{Name: p.Name, Label: p.Label, BaseURL: p.BaseURL, DefaultModel: p.DefaultModel, KeyHint: p.KeyHint})
	}
	if !out.Enabled {
		return out, nil
	}
	cred, err := a.h.aicreds.Get(ctx, userID)
	switch {
	case err == nil:
		out.Current = &AICurrent{
			Provider: cred.Provider, KeyHint: cred.KeyHint, Model: cred.Model, BaseURL: cred.BaseURL,
			LastError: cred.LastError, LastErrorAt: cred.LastErrorAt, SupportsTools: cred.SupportsTools, UpdatedAt: cred.UpdatedAt,
		}
	case apperr.Is(err, apperr.ErrNotFound):
	default:
		return AISettings{}, err
	}
	return out, nil
}

// MARK: Agent connections

// Connection is an agent (Claude Code, Codex, Hermes) allowed to reach this
// account over MCP. Its token is shown once, when created.
type Connection struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	TokenPrefix string     `json:"tokenPrefix"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

type ConnectionList struct {
	Connections []Connection `json:"connections"`
	// ConnectorURL is the MCP endpoint agents are pointed at.
	ConnectorURL string `json:"connectorUrl"`
}

type CreateConnectionRequest struct {
	Name string `json:"name"`
	// Kind is claude_code, codex, hermes or other.
	Kind string `json:"kind"`
}

// CreatedConnection carries the token in the only response that will ever
// hold it, with the setup the chosen client needs.
type CreatedConnection struct {
	Connection Connection      `json:"connection"`
	Token      string          `json:"token"`
	Setup      ConnectionSetup `json:"setup"`
}

type ConnectionSetup struct {
	URL         string `json:"url"`
	ConfigLabel string `json:"configLabel,omitempty"`
	ConfigLang  string `json:"configLang,omitempty"`
	Config      string `json:"config,omitempty"`
	Export      string `json:"export,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

func projectConnection(c connections.Connection) Connection {
	return Connection{ID: c.ID, Name: c.Name, Kind: string(c.Kind), TokenPrefix: c.TokenPrefix, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt}
}

func (a *API) listConnections(w http.ResponseWriter, r *http.Request) {
	list, err := a.h.connections.List(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Your connections could not be loaded.")
		return
	}
	out := ConnectionList{Connections: make([]Connection, 0, len(list)), ConnectorURL: a.h.connections.ConnectorURL()}
	for _, c := range list {
		out.Connections = append(out.Connections, projectConnection(c))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) createConnection(w http.ResponseWriter, r *http.Request) {
	var req CreateConnectionRequest
	if !read(w, r, &req) {
		return
	}
	issued, err := a.h.connections.Issue(r.Context(), auth.MustUser(r.Context()).ID, strings.TrimSpace(req.Name), connections.ClientKind(req.Kind))
	if err != nil {
		httpx.Error(w, err, "That connection could not be created.")
		return
	}
	setup := a.h.connections.Instructions(issued.Kind, issued.Token)
	httpx.WriteJSON(w, http.StatusCreated, CreatedConnection{
		Connection: projectConnection(issued.Connection),
		Token:      issued.Token,
		Setup: ConnectionSetup{
			URL: setup.URL, ConfigLabel: setup.ConfigLabel, ConfigLang: setup.ConfigLang,
			Config: setup.Config, Export: setup.Export, Prompt: setup.Prompt,
		},
	})
}

func (a *API) revokeConnection(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "connectionID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	if err := a.h.connections.Revoke(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "That connection could not be revoked.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Execution is one thing North did on this person's behalf, from the coach
// or an agent.
type Execution struct {
	ID        uuid.UUID       `json:"id"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	// Surface is coach or mcp.
	Surface string `json:"surface"`
	// Outcome is executed, failed or declined.
	Outcome   string    `json:"outcome"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type ActivityList struct {
	Executions []Execution `json:"executions"`
}

func (a *API) activity(w http.ResponseWriter, r *http.Request) {
	list, err := a.h.audit.List(r.Context(), auth.MustUser(r.Context()).ID, 0)
	if err != nil {
		httpx.Error(w, err, "Activity could not be loaded.")
		return
	}
	out := ActivityList{Executions: make([]Execution, 0, len(list))}
	for _, e := range list {
		args := e.Arguments
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		out.Executions = append(out.Executions, Execution{
			ID: e.ID, Tool: e.Tool, Arguments: args, Surface: string(e.Surface), Outcome: string(e.Outcome), Detail: e.Detail, CreatedAt: e.CreatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// MARK: Telegram

type TelegramSettings struct {
	// Enabled is false on a server with no bot configured.
	Enabled     bool       `json:"enabled"`
	BotUsername string     `json:"botUsername,omitempty"`
	Linked      bool       `json:"linked"`
	LinkedAt    *time.Time `json:"linkedAt,omitempty"`
	LastSeenAt  *time.Time `json:"lastSeenAt,omitempty"`
}

// TelegramCode links a Telegram account: open DeepLink, or send "/start
// <code>" to the bot. The code is single-use and short-lived.
type TelegramCode struct {
	Code     string `json:"code"`
	DeepLink string `json:"deepLink,omitempty"`
}

func (a *API) getTelegram(w http.ResponseWriter, r *http.Request) {
	out := TelegramSettings{Enabled: a.h.telegram.Enabled(), BotUsername: a.h.telegram.BotUsername}
	if out.Enabled {
		links, err := a.h.messaging.Links(r.Context(), auth.MustUser(r.Context()).ID)
		if err != nil {
			httpx.Error(w, err, "Telegram could not be checked.")
			return
		}
		for _, link := range links {
			if link.Platform == messaging.PlatformTelegram {
				out.Linked, out.LinkedAt, out.LastSeenAt = true, &link.CreatedAt, link.LastSeenAt
				break
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) telegramCode(w http.ResponseWriter, r *http.Request) {
	if !a.h.telegram.Enabled() {
		httpx.Error(w, apperr.ErrNotFound, "Telegram is not available on this server.")
		return
	}
	code, err := a.h.messaging.IssueCode(r.Context(), auth.MustUser(r.Context()).ID, messaging.PlatformTelegram)
	if err != nil {
		httpx.Error(w, err, "A link code could not be issued.")
		return
	}
	out := TelegramCode{Code: code}
	if a.h.telegram.BotUsername != "" {
		out.DeepLink = "https://t.me/" + url.PathEscape(a.h.telegram.BotUsername) + "?start=" + url.QueryEscape(code)
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) unlinkTelegram(w http.ResponseWriter, r *http.Request) {
	if _, err := a.h.messaging.Unlink(r.Context(), auth.MustUser(r.Context()).ID, messaging.PlatformTelegram); err != nil {
		httpx.Error(w, err, "Telegram could not be disconnected.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MARK: Calendar

type CalendarSettings struct {
	// Enabled is false on a server that cannot store the connection.
	Enabled   bool                `json:"enabled"`
	Connected *CalendarConnection `json:"connected,omitempty"`
}

type CalendarConnection struct {
	Provider      string     `json:"provider"`
	Endpoint      string     `json:"endpoint"`
	Status        string     `json:"status"`
	LastError     string     `json:"lastError,omitempty"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
}

type CalendarRequest struct {
	// Endpoint is the calendar's MCP server URL.
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

func (a *API) getCalendar(w http.ResponseWriter, r *http.Request) {
	out, err := a.calendar(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Your calendar could not be checked.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) putCalendar(w http.ResponseWriter, r *http.Request) {
	if a.h.integrations == nil {
		httpx.Error(w, apperr.ErrNotFound, "Calendars are not available on this server.")
		return
	}
	user := auth.MustUser(r.Context())
	var req CalendarRequest
	if !read(w, r, &req) {
		return
	}
	if err := a.h.integrations.Connect(r.Context(), user.ID, req.Endpoint, req.Token); err != nil {
		// Validation and unreachable-endpoint errors are written for the person.
		if apperr.Is(err, apperr.ErrValidation) || apperr.Is(err, apperr.ErrUnavailable) {
			httpx.Error(w, apperr.FieldErrors{}.Add("endpoint", err.Error()), err.Error())
			return
		}
		httpx.Error(w, err, "That calendar could not be connected.")
		return
	}
	out, err := a.calendar(r.Context(), user.ID)
	if err != nil {
		httpx.Error(w, err, "Your calendar could not be checked.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteCalendar(w http.ResponseWriter, r *http.Request) {
	if a.h.integrations == nil {
		httpx.Error(w, apperr.ErrNotFound, "Calendars are not available on this server.")
		return
	}
	if err := a.h.integrations.Disconnect(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Your calendar could not be disconnected.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) calendar(ctx context.Context, userID uuid.UUID) (CalendarSettings, error) {
	if a.h.integrations == nil {
		return CalendarSettings{}, nil
	}
	out := CalendarSettings{Enabled: true}
	conn, ok, err := a.h.integrations.Status(ctx, userID)
	if err != nil {
		return CalendarSettings{}, err
	}
	if ok {
		out.Connected = &CalendarConnection{Provider: conn.Provider, Endpoint: conn.Endpoint, Status: conn.Status, LastError: conn.LastError, LastCheckedAt: conn.LastCheckedAt}
	}
	return out, nil
}

// MARK: Account

type DeleteAccountRequest struct {
	// ConfirmEmail must match the account's email: typing it is the
	// confirmation, as on the web.
	ConfirmEmail string `json:"confirmEmail"`
}

// deleteAccount erases the account. The session goes with it, so the client
// signs out on a 204.
func (a *API) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var req DeleteAccountRequest
	if !read(w, r, &req) {
		return
	}
	if _, err := a.h.account.Delete(r.Context(), auth.MustUser(r.Context()), req.ConfirmEmail); err != nil {
		var fields apperr.FieldErrors
		if apperr.As(err, &fields) {
			httpx.Error(w, apperr.FieldErrors{}.Add("confirmEmail", fields.Messages()[account.ConfirmField]), "Type your email address to confirm.")
			return
		}
		httpx.Error(w, err, "Your account could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func read(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 64 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
