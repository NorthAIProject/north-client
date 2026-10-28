package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/account"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/theme"
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

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(h *Handler) *API {
	return &API{h: h}
}

func (a *API) Register(g *echo.Group) {
	g.GET("/settings/profile", a.getProfile)
	g.PUT("/settings/profile", a.putProfile)
	g.GET("/settings/preferences", a.getPreferences)
	g.PUT("/settings/preferences", a.putPreferences)
	g.PUT("/settings/target-weight", a.putTargetWeight)
	g.PUT("/settings/appearance", a.putAppearance)
	g.GET("/settings/notifications", a.getNotifications)
	g.PUT("/settings/notifications", a.putNotifications)

	g.GET("/settings/ai", a.getAI)
	g.PUT("/settings/ai", a.putAI)
	g.DELETE("/settings/ai", a.deleteAI)

	g.GET("/settings/connections", a.listConnections)
	g.POST("/settings/connections", a.createConnection)
	g.GET("/settings/connections/:connectionID", a.getConnection)
	g.DELETE("/settings/connections/:connectionID", a.revokeConnection)
	g.GET("/settings/activity", a.activity)

	g.GET("/settings/telegram", a.getTelegram)
	g.POST("/settings/telegram/code", a.telegramCode)
	g.DELETE("/settings/telegram", a.unlinkTelegram)

	g.GET("/settings/diets", a.getDiets)
	g.PUT("/settings/diets", a.putDiets)

	g.GET("/settings/calendar", a.getCalendar)
	g.PUT("/settings/calendar", a.putCalendar)
	g.DELETE("/settings/calendar", a.deleteCalendar)

	g.POST("/account/delete", a.deleteAccount)
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

	// Appearance is system, light or dark, and absent when the person never
	// chose: the app applies its own default then, so the server must not
	// report the web's. A pointer both ways — on a PUT, absent means
	// "unchanged", because the builds that predate it send the profile
	// without it.
	Appearance *string `json:"appearance,omitempty"`
}

func projectProfile(u users.User) Profile {
	p := Profile{
		Email: u.Email, DisplayName: u.DisplayName, Timezone: u.Timezone, Locale: string(u.Locale),
		CoachingStyle: u.CoachingStyle, CoachingTone: string(u.CoachingTone),
	}
	if u.Appearance != "" {
		a := string(u.Appearance)
		p.Appearance = &a
	}
	return p
}

// requestedAppearance carries a submitted appearance through unvalidated —
// users.Service refuses a bad one — and keeps "not sent" as nil.
func requestedAppearance(s *string) *theme.Appearance {
	if s == nil {
		return nil
	}
	a := theme.Appearance(*s)
	return &a
}

// AppearanceRequest changes the appearance alone. The web client's public
// switcher and its settings page need this: PUT /settings/profile replaces
// the whole profile, and the switcher has nothing else to send.
type AppearanceRequest struct {
	Appearance string `json:"appearance"`
	// Surface says where the change was made, for the funnel: "switcher"
	// (the public pages' toggle) or "settings". Anything else is settings.
	Surface string `json:"surface,omitempty"`
}

func (a *API) putAppearance(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var req AppearanceRequest
	if err := read(c, &req); err != nil {
		return err
	}
	surface := analytics.AppearanceFromSettings
	if req.Surface == "switcher" {
		surface = analytics.AppearanceFromSwitcher
	}
	if _, err := a.h.users.SetAppearance(ctx, user.ID, req.Appearance, surface); err != nil {
		return httpx.Fail(err, "Your appearance could not be saved.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) getProfile(c *echo.Context) error {
	ctx := c.Request().Context()
	return c.JSON(http.StatusOK, projectProfile(auth.MustUser(ctx)))
}

func (a *API) putProfile(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var req Profile
	if err := read(c, &req); err != nil {
		return err
	}
	updated, err := a.h.users.UpdateProfile(ctx, user.ID, users.Profile{
		DisplayName:   strings.TrimSpace(req.DisplayName),
		Timezone:      strings.TrimSpace(req.Timezone),
		Locale:        users.ResolveLocale(req.Locale),
		CoachingStyle: strings.TrimSpace(req.CoachingStyle),
		CoachingTone:  users.Tone(strings.TrimSpace(req.CoachingTone)),
		Appearance:    requestedAppearance(req.Appearance),
		Surface:       analytics.AppearanceFromIOS,
	})
	if err != nil {
		return httpx.Fail(err, "Your profile could not be saved.")
	}
	return c.JSON(http.StatusOK, projectProfile(updated))
}

// MARK: Preferences

type Preferences struct {
	// UnitsSystem is metric or imperial.
	UnitsSystem       string `json:"unitsSystem"`
	DefaultGoal       string `json:"defaultGoal"`
	DefaultMacroSplit string `json:"defaultMacroSplit"`
}

func (a *API) getPreferences(c *echo.Context) error {
	ctx := c.Request().Context()
	p, err := a.h.preferences.Get(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your preferences could not be loaded.")
	}
	return c.JSON(http.StatusOK, Preferences{UnitsSystem: p.UnitsSystem, DefaultGoal: p.DefaultGoal, DefaultMacroSplit: p.DefaultMacroSplit})
}

func (a *API) putPreferences(c *echo.Context) error {
	ctx := c.Request().Context()
	var req Preferences
	if err := read(c, &req); err != nil {
		return err
	}
	p, err := a.h.preferences.Upsert(ctx, auth.MustUser(ctx).ID, preferences.Input{
		UnitsSystem: req.UnitsSystem, DefaultGoal: req.DefaultGoal, DefaultMacroSplit: req.DefaultMacroSplit,
	})
	if err != nil {
		return httpx.Fail(err, "Your preferences could not be saved.")
	}
	return c.JSON(http.StatusOK, Preferences{UnitsSystem: p.UnitsSystem, DefaultGoal: p.DefaultGoal, DefaultMacroSplit: p.DefaultMacroSplit})
}

// TargetWeight is the weight being aimed at. Null clears it.
type TargetWeight struct {
	TargetWeightKg *float64 `json:"targetWeightKg"`
}

func (a *API) putTargetWeight(c *echo.Context) error {
	ctx := c.Request().Context()
	var req TargetWeight
	if err := read(c, &req); err != nil {
		return err
	}
	p, err := a.h.preferences.SetTargetWeight(ctx, auth.MustUser(ctx).ID, req.TargetWeightKg)
	if err != nil {
		return httpx.Fail(err, "The target weight could not be saved.")
	}
	return c.JSON(http.StatusOK, TargetWeight{TargetWeightKg: p.TargetWeightKg})
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
	// BriefingHour is the local hour, 0-23, the morning briefing arrives.
	BriefingHour int `json:"briefingHour"`
	// EveningReflection asks each evening, at EveningHour, how the day went.
	EveningReflection bool `json:"eveningReflection"`
	EveningHour       int  `json:"eveningHour"`
	// CoachCheckIns lets the coach message first, at most once a day, when
	// it notices something. CoachAdjustsTraining lets it lighten a session
	// itself on a low-recovery morning, with an undo; off, it only offers.
	CoachCheckIns        bool `json:"coachCheckIns"`
	CoachAdjustsTraining bool `json:"coachAdjustsTraining"`
}

// NotificationsUpdate is Notifications as a client sends it. The timing fields
// are optional so an app built before they existed keeps what is saved
// instead of resetting the briefing to midnight.
type NotificationsUpdate struct {
	Notifications
	BriefingHour      *int  `json:"briefingHour,omitempty"`
	EveningReflection *bool `json:"eveningReflection,omitempty"`
	EveningHour       *int  `json:"eveningHour,omitempty"`
	// Optional for the same reason: an app that predates them keeps what is
	// saved.
	CoachCheckIns        *bool `json:"coachCheckIns,omitempty"`
	CoachAdjustsTraining *bool `json:"coachAdjustsTraining,omitempty"`
}

func (a *API) getNotifications(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	n, err := a.h.notifications.Get(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, "Your notification settings could not be loaded.")
	}
	photo, err := a.h.notifications.PhotoSchedule(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, "Your notification settings could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectNotifications(n, photo))
}

func (a *API) putNotifications(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var req NotificationsUpdate
	if err := read(c, &req); err != nil {
		return err
	}
	n, err := a.h.notifications.Upsert(ctx, user.ID, notifications.Input{
		BriefingHour: req.BriefingHour, EveningReflection: req.EveningReflection, EveningHour: req.EveningHour,
		CoachCheckIns: req.CoachCheckIns, CoachAdjustsTraining: req.CoachAdjustsTraining,
		NudgeMissedCheckIn: req.NudgeMissedCheckIn, NudgeGoalDeadline: req.NudgeGoalDeadline,
		CoachActivity: req.CoachActivity, TrainingReminders: req.TrainingReminders,
		WeeklyReportAuto: req.WeeklyReportAuto, DailyBriefingAuto: req.DailyBriefingAuto,
		StatsDigestCadence: req.StatsDigestCadence,
		QuietHoursEnabled:  req.QuietHoursEnabled, QuietStart: strings.TrimSpace(req.QuietStart), QuietEnd: strings.TrimSpace(req.QuietEnd),
	})
	if err != nil {
		return httpx.Fail(err, "Your notification settings could not be saved.")
	}
	photo, err := a.h.notifications.UpsertSchedule(ctx, user.ID, notifications.ScheduleInput{
		Kind: notifications.KindPhoto, Enabled: req.PhotoAskEnabled, EveryDays: req.PhotoEveryDays, ReminderDays: req.PhotoReminderDays,
	})
	if err != nil {
		return httpx.Fail(err, "Your photo reminders could not be saved.")
	}
	return c.JSON(http.StatusOK, projectNotifications(n, photo))
}

func projectNotifications(n notifications.Prefs, photo notifications.Schedule) Notifications {
	return Notifications{
		NudgeMissedCheckIn: n.NudgeMissedCheckIn, NudgeGoalDeadline: n.NudgeGoalDeadline,
		CoachActivity: n.CoachActivity, TrainingReminders: n.TrainingReminders,
		WeeklyReportAuto: n.WeeklyReportAuto, DailyBriefingAuto: n.DailyBriefingAuto,
		StatsDigestCadence: n.StatsDigestCadence,
		QuietHoursEnabled:  n.QuietHoursEnabled, QuietStart: n.QuietStart, QuietEnd: n.QuietEnd,
		PhotoAskEnabled: photo.Enabled, PhotoEveryDays: photo.EveryDays, PhotoReminderDays: photo.ReminderDays,
		BriefingHour: n.BriefingHour, EveningReflection: n.EveningReflection, EveningHour: n.EveningHour,
		CoachCheckIns: n.CoachCheckIns, CoachAdjustsTraining: n.CoachAdjustsTraining,
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
	// Note is one line under the option, in the request's language.
	Note string `json:"note,omitempty"`
	// RequiresBaseURL is a backend with no public address of its own: the
	// person names their gateway.
	RequiresBaseURL bool `json:"requiresBaseUrl,omitempty"`
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

func (a *API) getAI(c *echo.Context) error {
	ctx := c.Request().Context()
	out, err := a.aiSettings(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your AI provider could not be loaded.")
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) putAI(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var req AIRequest
	if err := read(c, &req); err != nil {
		return err
	}
	if _, err := a.h.aicreds.Save(ctx, user.ID, aicreds.Input{
		Provider: req.Provider, APIKey: req.APIKey, Model: req.Model, BaseURL: req.BaseURL,
	}); err != nil {
		return httpx.Fail(err, "That provider could not be saved.")
	}
	// As on the web: find out in the background whether this provider will
	// call the coach's tools. The answer appears on a later GET.
	go func(userID uuid.UUID) {
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), probeBudget)
		defer cancel()
		a.h.aicreds.ProbeTools(probeCtx, userID)
	}(user.ID)

	out, err := a.aiSettings(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, "Your AI provider could not be loaded.")
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) deleteAI(c *echo.Context) error {
	ctx := c.Request().Context()
	// Already gone is the outcome the caller wanted.
	if err := a.h.aicreds.Delete(ctx, auth.MustUser(ctx).ID); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		return httpx.Fail(err, "Your AI provider could not be removed.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) aiSettings(ctx context.Context, userID uuid.UUID) (AISettings, error) {
	out := AISettings{Enabled: a.h.aicreds.Enabled(), Providers: []AIProvider{}}
	for _, p := range a.h.aicreds.Providers() {
		out.Providers = append(out.Providers, AIProvider{
			Name: p.Name, Label: p.Label, BaseURL: p.BaseURL, DefaultModel: p.DefaultModel, KeyHint: p.KeyHint,
			Note: p.NoteIn(i18n.LocaleFrom(ctx)), RequiresBaseURL: p.RequiresBaseURL,
		})
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
	// KindLabel is the client as written for a person ("Claude Code").
	KindLabel string `json:"kindLabel"`
	// ExpiresAt is set for an OAuth grant, whose access token lasts an hour
	// and is replaced in place: a past value means "needs refreshing".
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type ConnectionList struct {
	Connections []Connection `json:"connections"`
	// ConnectorURL is the MCP endpoint agents are pointed at.
	ConnectorURL string `json:"connectorUrl"`
	// Previews is the setup for each client, in the order the page offers
	// them, with a placeholder where the key goes: what connecting involves,
	// before a credential exists.
	Previews []ConnectionSetup `json:"previews"`
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
	// Kind and KindLabel name the client the setup is for.
	Kind      string `json:"kind,omitempty"`
	KindLabel string `json:"kindLabel,omitempty"`
	// Safe is the same configuration reading the token from an environment
	// variable (set by Export), for a config file somebody might commit.
	// SafeNote says why it exists. Empty for clients without a config file.
	SafeLabel string `json:"safeLabel,omitempty"`
	SafeLang  string `json:"safeLang,omitempty"`
	Safe      string `json:"safe,omitempty"`
	SafeNote  string `json:"safeNote,omitempty"`
}

func projectConnection(c connections.Connection) Connection {
	return Connection{
		ID: c.ID, Name: c.Name, Kind: string(c.Kind), TokenPrefix: c.TokenPrefix, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt,
		KindLabel: c.Kind.Label(), ExpiresAt: c.ExpiresAt,
	}
}

func projectSetup(s connections.Setup) ConnectionSetup {
	return ConnectionSetup{
		URL: s.URL, ConfigLabel: s.ConfigLabel, ConfigLang: s.ConfigLang, Config: s.Config, Export: s.Export, Prompt: s.Prompt,
		Kind: string(s.Kind), KindLabel: s.Kind.Label(),
		SafeLabel: s.SafeLabel, SafeLang: s.SafeLang, Safe: s.Safe, SafeNote: s.SafeNote,
	}
}

func (a *API) listConnections(c *echo.Context) error {
	ctx := c.Request().Context()
	list, err := a.h.connections.List(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your connections could not be loaded.")
	}
	out := ConnectionList{
		Connections:  make([]Connection, 0, len(list)),
		ConnectorURL: a.h.connections.ConnectorURL(),
		Previews:     make([]ConnectionSetup, 0, len(connections.ClientKinds)),
	}
	for _, conn := range list {
		out.Connections = append(out.Connections, projectConnection(conn))
	}
	locale := i18n.LocaleFrom(ctx)
	for _, kind := range connections.ClientKinds {
		out.Previews = append(out.Previews, projectSetup(a.h.connections.Preview(kind).In(locale)))
	}
	return c.JSON(http.StatusOK, out)
}

// getConnection answers the "waiting for first use" poll: lastUsedAt appears
// once an agent has authenticated with the key. A revoked one is a 404.
func (a *API) getConnection(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := echox.UUIDParam(c, "connectionID")
	if err != nil {
		return err
	}
	conn, err := a.h.connections.Get(ctx, id, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "That connection could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectConnection(conn))
}

func (a *API) createConnection(c *echo.Context) error {
	ctx := c.Request().Context()
	var req CreateConnectionRequest
	if err := read(c, &req); err != nil {
		return err
	}
	issued, err := a.h.connections.Issue(ctx, auth.MustUser(ctx).ID, strings.TrimSpace(req.Name), connections.ClientKind(req.Kind))
	if err != nil {
		return httpx.Fail(err, "That connection could not be created.")
	}
	setup := a.h.connections.Instructions(issued.Kind, issued.Token).In(i18n.LocaleFrom(ctx))
	return c.JSON(http.StatusCreated, CreatedConnection{
		Connection: projectConnection(issued.Connection),
		Token:      issued.Token,
		Setup:      projectSetup(setup),
	})
}

func (a *API) revokeConnection(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := echox.UUIDParam(c, "connectionID")
	if err != nil {
		return err
	}
	if err := a.h.connections.Revoke(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "That connection could not be revoked.")
	}
	return c.NoContent(http.StatusNoContent)
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

func (a *API) activity(c *echo.Context) error {
	ctx := c.Request().Context()
	list, err := a.h.audit.List(ctx, auth.MustUser(ctx).ID, 0)
	if err != nil {
		return httpx.Fail(err, "Activity could not be loaded.")
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
	return c.JSON(http.StatusOK, out)
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

func (a *API) getTelegram(c *echo.Context) error {
	ctx := c.Request().Context()
	out := TelegramSettings{Enabled: a.h.telegram.Enabled(), BotUsername: a.h.telegram.BotUsername}
	if out.Enabled {
		links, err := a.h.messaging.Links(ctx, auth.MustUser(ctx).ID)
		if err != nil {
			return httpx.Fail(err, "Telegram could not be checked.")
		}
		for _, link := range links {
			if link.Platform == messaging.PlatformTelegram {
				out.Linked, out.LinkedAt, out.LastSeenAt = true, &link.CreatedAt, link.LastSeenAt
				break
			}
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) telegramCode(c *echo.Context) error {
	ctx := c.Request().Context()
	if !a.h.telegram.Enabled() {
		return httpx.Fail(apperr.ErrNotFound, "Telegram is not available on this server.")
	}
	code, err := a.h.messaging.IssueCode(ctx, auth.MustUser(ctx).ID, messaging.PlatformTelegram)
	if err != nil {
		return httpx.Fail(err, "A link code could not be issued.")
	}
	out := TelegramCode{Code: code}
	if a.h.telegram.BotUsername != "" {
		out.DeepLink = "https://t.me/" + url.PathEscape(a.h.telegram.BotUsername) + "?start=" + url.QueryEscape(code)
	}
	return c.JSON(http.StatusCreated, out)
}

func (a *API) unlinkTelegram(c *echo.Context) error {
	ctx := c.Request().Context()
	if _, err := a.h.messaging.Unlink(ctx, auth.MustUser(ctx).ID, messaging.PlatformTelegram); err != nil {
		return httpx.Fail(err, "Telegram could not be disconnected.")
	}
	return c.NoContent(http.StatusNoContent)
}

// MARK: Diets

// DietOption is one diet the coach and the meal planner respect, and whether
// this person follows it.
type DietOption struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Selected    bool      `json:"selected"`
}

type DietSettings struct {
	Diets []DietOption `json:"diets"`
}

type DietsRequest struct {
	// DietIDs replaces the whole selection; empty clears it.
	DietIDs []uuid.UUID `json:"dietIds"`
}

func (a *API) getDiets(c *echo.Context) error {
	ctx := c.Request().Context()
	out, err := a.diets(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your diets could not be loaded.")
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) putDiets(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var req DietsRequest
	if err := read(c, &req); err != nil {
		return err
	}
	all, err := a.h.diets.ListDiets(ctx)
	if err != nil {
		return httpx.Fail(err, "Your diets could not be saved.")
	}
	// The web form can only post ids it rendered; a JSON client can send
	// anything, so an unknown id is named rather than left to the database.
	known := make(map[uuid.UUID]bool, len(all))
	for _, d := range all {
		known[d.ID] = true
	}
	for _, id := range req.DietIDs {
		if !known[id] {
			return httpx.Fail(apperr.FieldErrors{}.Add("dietIds", "That diet does not exist."), "That diet does not exist.")
		}
	}
	if err = a.h.diets.SetUserDiets(ctx, user.ID, req.DietIDs); err != nil {
		return httpx.Fail(err, "Your diets could not be saved.")
	}
	out, err := a.diets(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, "Your diets could not be loaded.")
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) diets(ctx context.Context, userID uuid.UUID) (DietSettings, error) {
	all, err := a.h.diets.ListDiets(ctx)
	if err != nil {
		return DietSettings{}, err
	}
	mine, err := a.h.diets.UserDiets(ctx, userID)
	if err != nil {
		return DietSettings{}, err
	}
	return projectDiets(all, mine), nil
}

func projectDiets(all, mine []meal.Diet) DietSettings {
	selected := make(map[uuid.UUID]bool, len(mine))
	for _, d := range mine {
		selected[d.ID] = true
	}
	out := DietSettings{Diets: make([]DietOption, 0, len(all))}
	for _, d := range all {
		out.Diets = append(out.Diets, DietOption{
			ID: d.ID, Code: d.Code, Name: d.Name, Description: d.Description, Selected: selected[d.ID],
		})
	}
	return out
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

func (a *API) getCalendar(c *echo.Context) error {
	ctx := c.Request().Context()
	out, err := a.calendar(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your calendar could not be checked.")
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) putCalendar(c *echo.Context) error {
	ctx := c.Request().Context()
	if a.h.integrations == nil {
		return httpx.Fail(apperr.ErrNotFound, "Calendars are not available on this server.")
	}
	user := auth.MustUser(ctx)
	var req CalendarRequest
	if err := read(c, &req); err != nil {
		return err
	}
	if err := a.h.integrations.Connect(ctx, user.ID, req.Endpoint, req.Token); err != nil {
		// Validation and unreachable-endpoint errors are written for the person.
		if apperr.Is(err, apperr.ErrValidation) || apperr.Is(err, apperr.ErrUnavailable) {
			return httpx.Fail(apperr.FieldErrors{}.Add("endpoint", err.Error()), err.Error())
		}
		return httpx.Fail(err, "That calendar could not be connected.")
	}
	out, err := a.calendar(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, "Your calendar could not be checked.")
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) deleteCalendar(c *echo.Context) error {
	ctx := c.Request().Context()
	if a.h.integrations == nil {
		return httpx.Fail(apperr.ErrNotFound, "Calendars are not available on this server.")
	}
	if err := a.h.integrations.Disconnect(ctx, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "Your calendar could not be disconnected.")
	}
	return c.NoContent(http.StatusNoContent)
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
func (a *API) deleteAccount(c *echo.Context) error {
	ctx := c.Request().Context()
	var req DeleteAccountRequest
	if err := read(c, &req); err != nil {
		return err
	}
	if _, err := a.h.account.Delete(ctx, auth.MustUser(ctx), req.ConfirmEmail); err != nil {
		var fields apperr.FieldErrors
		if apperr.As(err, &fields) {
			return httpx.Fail(apperr.FieldErrors{}.Add("confirmEmail", fields.Messages()[account.ConfirmField]), "Type your email address to confirm.")
		}
		return httpx.Fail(err, "Your account could not be deleted.")
	}
	return c.NoContent(http.StatusNoContent)
}

func read(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 64 << 10}, "The request body could not be read.")
}
