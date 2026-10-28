package soreness

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
)

// API is today's soreness for native clients.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/soreness", a.today)
	g.PUT("/soreness/:region", a.set)
	g.DELETE("/soreness/:region", a.clear)
}

type SorenessRequest struct {
	// Severity is 1 (stiff) to 3 (painful).
	Severity int    `json:"severity"`
	Note     string `json:"note,omitempty"`
}

type SorenessEntryView struct {
	Region   string `json:"region"`
	Severity int    `json:"severity"`
	Note     string `json:"note,omitempty"`
}

type SorenessTodayView struct {
	Entries []SorenessEntryView `json:"entries"`
	// Regions is every region, head to foot.
	Regions []string `json:"regions"`
}

func (a *API) today(c *echo.Context) error { return a.respond(c, http.StatusOK) }

func (a *API) set(c *echo.Context) error {
	var req SorenessRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := a.svc.Set(ctx, auth.MustUser(ctx), c.Param("region"), req.Severity, req.Note); err != nil {
		return httpx.Fail(err, "The soreness could not be saved.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) clear(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := a.svc.Clear(ctx, auth.MustUser(ctx), c.Param("region")); err != nil {
		return httpx.Fail(err, "The soreness could not be cleared.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) respond(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	entries, err := a.svc.OnDate(ctx, auth.MustUser(ctx), time.Now())
	if err != nil {
		return httpx.Fail(err, "Today's soreness could not be loaded.")
	}
	return c.JSON(status, ProjectToday(entries))
}

// ProjectToday is the today payload.
func ProjectToday(entries []Entry) SorenessTodayView {
	out := SorenessTodayView{Entries: make([]SorenessEntryView, len(entries)), Regions: sore.Regions()}
	for i, e := range entries {
		out.Entries[i] = SorenessEntryView{Region: e.Region, Severity: e.Severity, Note: e.Note}
	}
	return out
}
