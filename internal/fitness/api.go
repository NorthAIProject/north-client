package fitness

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// NativeReturnURL is where the Strava callback sends the app's sign-in sheet
// when it is done. The sheet closes on the khepri:// scheme and the app reads
// the result from the query.
const NativeReturnURL = "khepri://fitness/strava"

// API is Strava for native clients: status, connect, sync and disconnect, the
// same operations as the web fitness hub's Strava card.
type API struct {
	strava  *strava.Service
	baseURL string
	hub     *Service
}

// NewAPI builds the routes. Register goes behind auth.Bearer; RegisterPublic
// is the OAuth callback, which Strava calls with no session at all.
func NewAPI(svc *strava.Service, baseURL string) *API {
	return &API{strava: svc, baseURL: baseURL}
}

func (a *API) Register(g *echo.Group) {
	g.GET("/fitness/strava", a.status)
	g.POST("/fitness/strava/connect", a.connect)
	g.POST("/fitness/strava/sync", a.sync)
	g.DELETE("/fitness/strava", a.disconnect)

	// The web app's pages (hub_api.go).
	g.GET("/fitness/hub", a.hubView)
	g.GET("/fitness/activities", a.activities)
	g.GET("/fitness/activities/sessions", a.activitySessions)
	g.POST("/fitness/strava/connect/web", a.connectWeb)
	g.POST("/fitness/strava/connect/web/finish", a.finishWeb)
}

func (a *API) RegisterPublic(g *echo.Group) {
	g.GET("/fitness/strava/callback", a.callback)
}

type StravaStatus struct {
	// Configured is false when this deployment has no Strava credentials; the
	// app then hides the connection instead of offering one that cannot work.
	Configured bool `json:"configured"`
	Connected  bool `json:"connected"`
	// Unavailable means the connection could not be read; the web hub then
	// offers no Connect button, which would replace a working credential.
	Unavailable         bool       `json:"unavailable,omitempty"`
	LastSyncedAt        *time.Time `json:"lastSyncedAt,omitempty"`
	LastSyncAttemptedAt *time.Time `json:"lastSyncAttemptedAt,omitempty"`
	// LastSyncError is empty after a successful sync.
	LastSyncError string `json:"lastSyncError,omitempty"`
	// SyncPending means a sync is queued or running.
	SyncPending bool `json:"syncPending"`
}

type StravaConnect struct {
	// AuthorizeURL opens in the app's sign-in sheet; Strava returns to
	// NativeReturnURL with result=connected, cancelled, expired or failed.
	AuthorizeURL string `json:"authorizeUrl"`
}

func (a *API) status(c *echo.Context) error {
	ctx := c.Request().Context()
	s, err := a.strava.Status(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Strava's status could not be loaded.")
	}
	return c.JSON(http.StatusOK, StravaStatus{
		Configured: s.Configured, Connected: s.Connected,
		LastSyncedAt: s.LastSyncedAt, LastSyncAttemptedAt: s.LastSyncAttemptedAt,
		LastSyncError: s.LastSyncError, SyncPending: s.SyncPending,
	})
}

func (a *API) connect(c *echo.Context) error {
	ctx := c.Request().Context()
	consent, err := a.strava.BeginNativeConnect(ctx, auth.MustUser(ctx).ID, a.baseURL)
	if err != nil {
		return httpx.Fail(err, "Strava is not available right now.")
	}
	return c.JSON(http.StatusOK, StravaConnect{AuthorizeURL: consent})
}

func (a *API) sync(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := a.strava.RequestSync(ctx, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "Connect Strava first.")
	}
	return c.NoContent(http.StatusAccepted)
}

func (a *API) disconnect(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := a.strava.Disconnect(ctx, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "Strava could not be disconnected.")
	}
	return c.NoContent(http.StatusNoContent)
}

// callback finishes a connection begun by connect and hands the result back to
// the app. It always redirects to the app, never renders: the sign-in sheet
// shows nothing of its own, so the app is the only place to say what happened.
func (a *API) callback(c *echo.Context) error {
	ctx := c.Request().Context()
	result := "connected"
	switch {
	case c.QueryParam("error") != "":
		result = "cancelled"
	default:
		err := a.strava.FinishNativeConnect(ctx, c.QueryParam("state"), c.QueryParam("code"))
		switch {
		case apperr.Is(err, apperr.ErrNotFound):
			result = "expired"
		case err != nil:
			middleware.FromContext(ctx).Error("native strava connect failed", slog.Any("error", err))
			result = "failed"
		}
	}
	return c.Redirect(http.StatusFound, NativeReturnURL+"?"+url.Values{"result": {result}}.Encode())
}
