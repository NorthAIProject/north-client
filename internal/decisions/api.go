package decisions

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the decision log for native clients: what was decided, between
// what, why, and how it turned out, as the web /decisions page keeps it.
type API struct {
	svc *Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/decisions", a.list)
	g.POST("/decisions", a.create)
	g.GET("/decisions/calibration", a.calibration)
	g.GET("/decisions/:decisionID", a.show)
	g.PUT("/decisions/:decisionID", a.update)
	g.DELETE("/decisions/:decisionID", a.destroy)
}

type DecisionView struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Options   string    `json:"options"`
	Rationale string    `json:"rationale"`
	// Outcome is filled in later, once it is known how it went.
	Outcome string `json:"outcome"`
	// Held is whether the call held up when looked back on (yes, partly or
	// no); absent until answered.
	Held      string    `json:"held,omitempty"`
	DecidedAt time.Time `json:"decidedAt"`
}

// CalibrationView counts how calls held up among those looked back on.
type CalibrationView struct {
	Yes       int `json:"yes"`
	Partly    int `json:"partly"`
	No        int `json:"no"`
	Revisited int `json:"revisited"`
}

type DecisionList struct {
	Decisions []DecisionView `json:"decisions"`
}

type DecisionRequest struct {
	Title     string `json:"title"`
	Options   string `json:"options"`
	Rationale string `json:"rationale"`
	Outcome   string `json:"outcome"`
	// Held is yes, partly or no; empty or absent clears it.
	Held string `json:"held"`
}

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	list, err := a.svc.List(ctx, auth.MustUser(ctx).ID, 100)
	if err != nil {
		return httpx.Fail(err, "Decisions could not be loaded.")
	}
	out := DecisionList{Decisions: make([]DecisionView, 0, len(list))}
	for _, d := range list {
		out.Decisions = append(out.Decisions, project(d))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) create(c *echo.Context) error {
	var req DecisionRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	d, err := a.svc.Create(ctx, auth.MustUser(ctx).ID, Input(req))
	if err != nil {
		return httpx.Fail(err, "The decision could not be saved.")
	}
	return c.JSON(http.StatusCreated, project(d))
}

func (a *API) show(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "decisionID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	d, err := a.svc.Get(ctx, id, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The decision could not be loaded.")
	}
	return c.JSON(http.StatusOK, project(d))
}

func (a *API) update(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "decisionID")
	if err != nil {
		return err
	}
	var req DecisionRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	d, err := a.svc.Update(ctx, id, auth.MustUser(ctx).ID, Input(req))
	if err != nil {
		return httpx.Fail(err, "The decision could not be saved.")
	}
	return c.JSON(http.StatusOK, project(d))
}

func (a *API) calibration(c *echo.Context) error {
	ctx := c.Request().Context()
	cal, err := a.svc.Calibration(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Calibration could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectCalibration(cal))
}

func projectCalibration(c Calibration) CalibrationView {
	return CalibrationView{Yes: c.Yes, Partly: c.Partly, No: c.No, Revisited: c.Revisited()}
}

func (a *API) destroy(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "decisionID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.Delete(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The decision could not be deleted.")
	}
	return c.NoContent(http.StatusNoContent)
}

func project(d Decision) DecisionView {
	return DecisionView{ID: d.ID, Title: d.Title, Options: d.Options, Rationale: d.Rationale, Outcome: d.Outcome, Held: d.Held, DecidedAt: d.DecidedAt}
}

func readJSON(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 64 << 10}, "The request body could not be read.")
}
