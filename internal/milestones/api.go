package milestones

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is "months since" trackers for native clients, at /trackers: goals
// already have milestones, and the two must not be confused on the wire. Every change answers
// with the whole list.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/trackers", a.list)
	g.POST("/trackers", a.create)
	g.POST("/trackers/:id/done", a.done)
	g.DELETE("/trackers/:id", a.remove)
}

type MilestoneRequest struct {
	Name string `json:"name"`
	// LastDoneOn is YYYY-MM-DD; today when absent.
	LastDoneOn     string `json:"lastDoneOn,omitempty"`
	IntervalMonths *int   `json:"intervalMonths,omitempty"`
}

type MilestoneView struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	LastDoneOn     string    `json:"lastDoneOn"`
	IntervalMonths *int      `json:"intervalMonths,omitempty"`
	MonthsSince    int       `json:"monthsSince"`
	Due            bool      `json:"due"`
}

type MilestonesView struct {
	Trackers []MilestoneView `json:"trackers"`
}

func (a *API) list(c *echo.Context) error { return a.respond(c, http.StatusOK) }

func (a *API) create(c *echo.Context) error {
	var req MilestoneRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var last *time.Time
	if req.LastDoneOn != "" {
		d, err := time.ParseInLocation("2006-01-02", req.LastDoneOn, user.Location())
		if err != nil {
			return httpx.Fail(apperr.FieldErrors{}.Add("lastDoneOn", "Use YYYY-MM-DD."), "Use YYYY-MM-DD.")
		}
		last = &d
	}
	if _, err := a.svc.Create(ctx, user, req.Name, last, req.IntervalMonths); err != nil {
		return httpx.Fail(err, "The tracker could not be created.")
	}
	return a.respond(c, http.StatusCreated)
}

func (a *API) done(c *echo.Context) error {
	id, err := trackerID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := a.svc.Done(ctx, auth.MustUser(ctx), id); err != nil {
		return httpx.Fail(err, "The tracker could not be updated.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) remove(c *echo.Context) error {
	id, err := trackerID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.Delete(ctx, auth.MustUser(ctx), id); err != nil {
		return httpx.Fail(err, "The tracker could not be removed.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) respond(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	list, err := a.svc.List(ctx, user)
	if err != nil {
		return httpx.Fail(err, "The trackers could not be loaded.")
	}
	return c.JSON(status, ProjectList(list, time.Now().In(user.Location())))
}

// trackerID reads the :id path parameter. It answers with this API's own
// "No such tracker." rather than echox.UUIDParam's generic "Not found.".
func trackerID(c *echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, httpx.Fail(apperr.ErrNotFound, "No such tracker.")
	}
	return id, nil
}

// ProjectList is the list payload.
func ProjectList(list []Tracker, now time.Time) MilestonesView {
	out := MilestonesView{Trackers: make([]MilestoneView, len(list))}
	for i, t := range list {
		out.Trackers[i] = MilestoneView{
			ID: t.ID, Name: t.Name, LastDoneOn: t.LastDoneOn.Format("2006-01-02"),
			IntervalMonths: t.IntervalMonths, MonthsSince: t.MonthsSince(now), Due: t.Due(now),
		}
	}
	return out
}
