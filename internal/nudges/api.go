package nudges

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// API is the web's notification bell for native clients: the open nudges,
// how many are unread, and opening, reading or dismissing one.
type API struct {
	svc *Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/nudges", a.list)
	g.POST("/nudges/:nudgeID/open", a.open)
	g.POST("/nudges/:nudgeID/read", a.read)
	g.POST("/nudges/:nudgeID/dismiss", a.dismiss)
}

type NudgeView struct {
	ID    uuid.UUID `json:"id"`
	Kind  string    `json:"kind"`
	Title string    `json:"title"`
	Body  string    `json:"body"`
	// Href is the web path it leads to; the app maps it onto a screen.
	Href      string    `json:"href"`
	Unread    bool      `json:"unread"`
	CreatedAt time.Time `json:"createdAt"`
}

type NudgeList struct {
	Nudges []NudgeView `json:"nudges"`
	Unread int         `json:"unread"`
}

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	list, err := a.svc.ListOpen(ctx, userID, listDefault)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "nudges.err.load"))
	}
	unread, err := a.svc.CountUnread(ctx, userID)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "nudges.err.load"))
	}
	out := NudgeList{Nudges: make([]NudgeView, 0, len(list)), Unread: unread}
	for _, n := range list {
		out.Nudges = append(out.Nudges, project(n))
	}
	return c.JSON(http.StatusOK, out)
}

// open records which channel the nudge was followed from and marks it read.
// The bell is the default; ?from=push is the app opening a notification tap,
// so the funnel credits the lock screen and not the bell.
func (a *API) open(c *echo.Context) error {
	id, err := nudgeID(c)
	if err != nil {
		return err
	}
	channel := analytics.ChannelBell
	if c.QueryParam("from") == analytics.ChannelPush {
		channel = analytics.ChannelPush
	}
	ctx := c.Request().Context()
	n, err := a.svc.Open(ctx, id, auth.MustUser(ctx).ID, channel)
	return respond(c, n, err)
}

func (a *API) read(c *echo.Context) error {
	id, err := nudgeID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	n, err := a.svc.MarkRead(ctx, id, auth.MustUser(ctx).ID)
	return respond(c, n, err)
}

func (a *API) dismiss(c *echo.Context) error {
	id, err := nudgeID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	n, err := a.svc.Dismiss(ctx, id, auth.MustUser(ctx).ID)
	return respond(c, n, err)
}

func respond(c *echo.Context, n Nudge, err error) error {
	if err != nil {
		return httpx.Fail(err, i18n.T(c.Request().Context(), "nudges.err.update"))
	}
	return c.JSON(http.StatusOK, project(n))
}

func project(n Nudge) NudgeView {
	return NudgeView{ID: n.ID, Kind: n.Kind, Title: n.Title, Body: n.Body, Href: n.Href, Unread: n.Unread(), CreatedAt: n.CreatedAt}
}

func nudgeID(c *echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("nudgeID"))
	if err != nil {
		return uuid.Nil, httpx.Fail(apperr.ErrNotFound, i18n.T(c.Request().Context(), "nudges.err.notfound"))
	}
	return id, nil
}
