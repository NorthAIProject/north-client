package fasting

import (
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/fasting/fast"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is fasting for native clients. Every change answers with the current
// state, so the phone never works out a phase itself.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/fasting", a.show)
	g.POST("/fasting/start", a.start)
	g.POST("/fasting/stop", a.stop)
}

type FastStartRequest struct {
	// TargetHours defaults to 16.
	TargetHours int        `json:"targetHours,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
}

type FastView struct {
	ID             uuid.UUID  `json:"id"`
	StartedAt      time.Time  `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	TargetHours    int        `json:"targetHours"`
	ElapsedMinutes int        `json:"elapsedMinutes"`
	// Phase is fed, fasting, fat_burning or ketosis.
	Phase string `json:"phase"`
}

type FastingView struct {
	Current *FastView `json:"current,omitempty"`
}

func (a *API) show(c *echo.Context) error { return a.respond(c, http.StatusOK) }

func (a *API) start(c *echo.Context) error {
	var req FastStartRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := a.svc.Start(ctx, auth.MustUser(ctx), req.TargetHours, req.StartedAt); err != nil {
		return httpx.Fail(err, "The fast could not be started.")
	}
	return a.respond(c, http.StatusCreated)
}

func (a *API) stop(c *echo.Context) error {
	ctx := c.Request().Context()
	if _, err := a.svc.Stop(ctx, auth.MustUser(ctx)); err != nil {
		return httpx.Fail(err, "There is no fast to end.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) respond(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	current, ok, err := a.svc.Current(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, "Fasting could not be loaded.")
	}
	out := FastingView{}
	if ok {
		out.Current = util.Ptr(ProjectFast(current, time.Now()))
	}
	return c.JSON(status, out)
}

// ProjectFast is one fast's JSON shape.
func ProjectFast(s Session, now time.Time) FastView {
	elapsed := s.Elapsed(now)
	return FastView{
		ID: s.ID, StartedAt: s.StartedAt, EndedAt: s.EndedAt, TargetHours: s.TargetHours,
		ElapsedMinutes: int(elapsed.Minutes()), Phase: string(fast.PhaseAt(elapsed)),
	}
}
