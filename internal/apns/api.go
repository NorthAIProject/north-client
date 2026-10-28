package apns

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API lets the app register the device token iOS handed it, and forget it
// on sign-out. Register behind auth.Bearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.PUT("/devices/apns", a.register)
	g.DELETE("/devices/apns/:token", a.unregister)
}

type RegisterRequest struct {
	Token       string `json:"token"`
	Topic       string `json:"topic"`
	Environment string `json:"environment"`
}

func (a *API) register(c *echo.Context) error {
	var req RegisterRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	in := Input(req)
	ctx := c.Request().Context()
	if _, err := a.svc.Register(ctx, auth.MustUser(ctx).ID, in); err != nil {
		return httpx.Fail(err, "This device could not be registered for notifications.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) unregister(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := a.svc.Unregister(ctx, auth.MustUser(ctx).ID, c.Param("token")); err != nil {
		return httpx.Fail(err, "This device could not be removed.")
	}
	return c.NoContent(http.StatusNoContent)
}
