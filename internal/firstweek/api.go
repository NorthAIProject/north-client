package firstweek

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// API is the first week for native clients. Register behind auth.Bearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/first-week", a.get)
	g.POST("/first-week/start", a.start)
	g.POST("/first-week/done", a.done)
	g.POST("/first-week/skip", a.skip)
	g.POST("/first-week/finish", a.finish)
}

// View is the person's first week. Status, Template and Via are open
// strings: a value an older app does not know must not break decoding the
// whole response.
type View struct {
	// Status is active, finished, skipped, abandoned, or none when the person
	// never had a first week.
	Status string `json:"status"`
	// Template is sleep, train, protein, journal or custom.
	Template string `json:"template,omitempty"`
	// Day is 0 on the start date, 1 to 7 during the week, more after it.
	Day       int        `json:"day"`
	TotalDays int        `json:"totalDays"`
	StartedOn string     `json:"startedOn,omitempty"`
	GoalID    *uuid.UUID `json:"goalId,omitempty"`
	// Action is the daily action, in the person's language.
	Action string `json:"action,omitempty"`
	// Done is today's action done; false outside days 1 to 7.
	Done      bool      `json:"done"`
	DoneCount int       `json:"doneCount"`
	Days      []DayView `json:"days"`
	// Review is the day-7 review, once written. Opening it is
	// POST /first-week/finish.
	Review *ReviewView `json:"review,omitempty"`
}

// ReviewView is the week looked back on. WrittenBy is an open string: coach
// or template today.
type ReviewView struct {
	Text      string `json:"text"`
	WrittenBy string `json:"writtenBy"`
	DaysDone  int    `json:"daysDone"`
	DaysByLog int    `json:"daysByLog"`
	DaysByTap int    `json:"daysByTap"`
	// LoggedDays is how many of the seven days the template's log turned
	// up; absent when the template has no log to check.
	LoggedDays *int      `json:"loggedDays,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// DayView is one of the seven days.
type DayView struct {
	Day  int    `json:"day"`
	Date string `json:"date"`
	Done bool   `json:"done"`
	// Via is log or tap once done.
	Via string `json:"via,omitempty"`
}

// StartRequest begins a week. GoalID is optional: absent, the running week's
// goal is kept, or one is created from the template.
type StartRequest struct {
	Template string     `json:"template"`
	GoalID   *uuid.UUID `json:"goalId,omitempty"`
}

func (a *API) get(c *echo.Context) error {
	ctx := c.Request().Context()
	t, ok, err := a.svc.Today(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, "Your first week could not be loaded.")
	}
	if !ok {
		return c.JSON(http.StatusOK, None())
	}
	return c.JSON(http.StatusOK, Project(ctx, t))
}

func (a *API) start(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var req StartRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 1 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	goal := uuid.Nil
	if req.GoalID != nil {
		goal = *req.GoalID
	}
	if _, err := a.svc.Start(ctx, user, req.Template, goal); err != nil {
		return httpx.Fail(err, "Your first week could not be started.")
	}
	return a.get(c)
}

func (a *API) done(c *echo.Context) error {
	ctx := c.Request().Context()
	t, err := a.svc.TapDone(ctx, auth.MustUser(ctx))
	return a.write(c, t, err, "Today could not be marked done.")
}

func (a *API) skip(c *echo.Context) error {
	ctx := c.Request().Context()
	t, err := a.svc.Skip(ctx, auth.MustUser(ctx))
	return a.write(c, t, err, "Your first week could not be skipped.")
}

func (a *API) finish(c *echo.Context) error {
	ctx := c.Request().Context()
	t, err := a.svc.Finish(ctx, auth.MustUser(ctx))
	return a.write(c, t, err, "Your first week could not be finished.")
}

func (a *API) write(c *echo.Context, t Today, err error, msg string) error {
	if err != nil {
		return httpx.Fail(err, msg)
	}
	return c.JSON(http.StatusOK, Project(c.Request().Context(), t))
}

// None is the view for a person with no first week.
func None() View {
	return View{Status: StatusNone, TotalDays: Days, Days: []DayView{}}
}

// Project turns the week into its wire shape.
func Project(ctx context.Context, t Today) View {
	p := t.Programme
	v := View{
		Status: p.Status, Template: p.Template, Day: t.Day, TotalDays: Days,
		StartedOn: p.StartedOn.Format(time.DateOnly), GoalID: p.GoalID,
		Action: i18n.T(ctx, "firstweek."+p.Template+".action"),
		Done:   t.Done, DoneCount: t.DoneCount(), Days: make([]DayView, 0, len(t.Days)),
	}
	for _, d := range t.Days {
		v.Days = append(v.Days, DayView{Day: d.Day, Date: d.Date.Format(time.DateOnly), Done: d.Done, Via: d.Via})
	}
	if rv := p.Review; rv != nil {
		v.Review = &ReviewView{
			Text: rv.Text, WrittenBy: rv.WrittenBy, DaysDone: rv.DaysDone, DaysByLog: rv.DaysByLog,
			DaysByTap: rv.DaysByTap, LoggedDays: rv.LoggedDays, CreatedAt: rv.CreatedAt.UTC(),
		}
	}
	return v
}
