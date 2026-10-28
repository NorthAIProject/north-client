package push

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is Web Push opt-in for web clients over /api/v1: the same subscribe and
// unsubscribe the cookie-session handler serves, plus the VAPID key the
// browser subscribes with. Register it behind auth.Bearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/push/config", a.config)
	g.POST("/push/subscriptions", a.subscribe)
	g.DELETE("/push/subscriptions", a.unsubscribe)
}

// PushConfig says whether this deployment sends Web Push, with what key, and
// how many of the caller's browsers already listen.
type PushConfig struct {
	Enabled bool `json:"enabled"`
	// PublicKey is the VAPID applicationServerKey, base64url; empty when not enabled.
	PublicKey string `json:"publicKey"`
	Devices   int    `json:"devices"`
}

// apiSubscription is PushSubscription.toJSON(), plus the browser's user agent
// for a client calling through a proxy that does not forward it.
type apiSubscription struct {
	subscriptionJSON
	UserAgent string `json:"userAgent"`
}

func (a *API) config(c *echo.Context) error {
	ctx := c.Request().Context()
	devices, err := a.svc.Count(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Notifications could not be loaded.")
	}
	return c.JSON(http.StatusOK, PushConfig{Enabled: a.svc.Enabled(), PublicKey: a.svc.PublicKey(), Devices: devices})
}

func (a *API) subscribe(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	if !a.svc.Enabled() {
		return httpx.Fail(apperr.ErrNotFound, "Notifications are not available here.")
	}
	var in apiSubscription
	if ok, err := readSubscription(c, &in); !ok {
		return err
	}
	ua := in.UserAgent
	if ua == "" {
		ua = r.UserAgent()
	}
	_, err := a.svc.Subscribe(ctx, auth.MustUser(ctx).ID, Input{
		Endpoint: in.Endpoint, P256dh: in.Keys.P256dh, Auth: in.Keys.Auth, UserAgent: ua,
	})
	switch {
	case err == nil:
		return c.NoContent(http.StatusNoContent)
	case apperr.Is(err, apperr.ErrValidation):
		return httpx.Fail(err, "That is not a valid push subscription.")
	case apperr.Is(err, apperr.ErrUnavailable):
		return httpx.Fail(apperr.ErrNotFound, "Notifications are not available here.")
	default:
		return httpx.Fail(err, "Something went wrong.")
	}
}

func (a *API) unsubscribe(c *echo.Context) error {
	ctx := c.Request().Context()
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if ok, err := readSubscription(c, &in); !ok {
		return err
	}
	if in.Endpoint == "" {
		return httpx.Fail(apperr.ErrValidation, "An endpoint is required.")
	}
	if err := a.svc.Unsubscribe(ctx, auth.MustUser(ctx).ID, in.Endpoint); err != nil {
		return httpx.Fail(err, "Something went wrong.")
	}
	return c.NoContent(http.StatusNoContent)
}

// readSubscription decodes a small JSON body, answering the request itself
// when it is not one: ok is then false, and err is only what writing that
// answer returned. Unknown fields are allowed for the reason the cookie
// handler gives: the browser's shape, not ours.
func readSubscription(c *echo.Context, dst any) (ok bool, err error) {
	err = httpx.ReadJSON(c.Response(), c.Request(), dst, httpx.ReadOptions{MaxBytes: maxRequestBytes, AllowUnknownFields: true})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, httpx.ErrTooLarge):
		return false, c.JSON(http.StatusRequestEntityTooLarge, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "That request is too large."}})
	default:
		return false, c.JSON(http.StatusBadRequest, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "The request body must be a JSON subscription."}})
	}
}
