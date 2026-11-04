package aiconsent

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API serves the consent question. Register it behind auth.Bearer.
type API struct {
	svc        *Service
	recipients []string
}

// NewAPI takes the provider names the registry holds.
func NewAPI(svc *Service, providerNames []string) *API {
	return &API{svc: svc, recipients: Recipients(providerNames)}
}

func (a *API) Register(g *echo.Group) {
	g.GET("/ai-consent", a.get)
	g.PUT("/ai-consent", a.put)
}

// View is what a client needs to ask the question and show the answer. Status
// is an open string on purpose: a shipped iOS app must survive a new value.
type View struct {
	Status     string     `json:"status"`
	Version    int        `json:"version"`
	DecidedAt  *time.Time `json:"decided_at,omitempty"`
	Recipients []string   `json:"recipients"`
}

// Request is the decision: granted or declined.
type Request struct {
	Decision string `json:"decision"`
}

func (a *API) view(c Consent) View {
	return View{Status: c.Status, Version: Version, DecidedAt: c.DecidedAt, Recipients: a.recipients}
}

func (a *API) get(c *echo.Context) error {
	ctx := c.Request().Context()
	consent, err := a.svc.Get(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your AI data sharing choice could not be loaded.")
	}
	return c.JSON(http.StatusOK, a.view(consent))
}

func (a *API) put(c *echo.Context) error {
	ctx := c.Request().Context()
	var req Request
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 1 << 10}, "That choice could not be read."); err != nil {
		return err
	}
	consent, err := a.svc.Set(ctx, auth.MustUser(ctx).ID, req.Decision)
	if err != nil {
		return httpx.Fail(err, "Your AI data sharing choice could not be saved.")
	}
	return c.JSON(http.StatusOK, a.view(consent))
}
