package news

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// The ticker's settings, for web clients over /api/v1: the switch and the
// feeds the person follows, which the cookie-session settings page serves at
// /app/settings/news-ticker.

type FeedView struct {
	ID uuid.UUID `json:"id"`
	// Title is the feed's own name once fetched; empty before.
	Title string `json:"title"`
	URL   string `json:"url"`
}

type NewsSettings struct {
	Enabled  bool       `json:"enabled"`
	Feeds    []FeedView `json:"feeds"`
	MaxFeeds int        `json:"maxFeeds"`
}

type newsSettingsInput struct {
	Enabled bool `json:"enabled"`
}

type feedInput struct {
	URL string `json:"url"`
}

func (a *API) registerSettings(g *echo.Group) {
	g.GET("/news/settings", a.settings)
	g.PUT("/news/settings", a.setSettings)
	g.POST("/news/feeds", a.addFeed)
	g.DELETE("/news/feeds/:feedID", a.removeFeed)
}

func (a *API) settings(c *echo.Context) error {
	if err := a.available(); err != nil {
		return err
	}
	return a.writeSettings(c, http.StatusOK)
}

func (a *API) setSettings(c *echo.Context) error {
	if err := a.available(); err != nil {
		return err
	}
	var in newsSettingsInput
	if err := readInput(c, &in, 1<<10); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.SetEnabled(ctx, auth.MustUser(ctx), in.Enabled); err != nil {
		return httpx.Fail(err, "The news settings could not be saved.")
	}
	return a.writeSettings(c, http.StatusOK)
}

// addFeed answers 422 with fields.url when the address is refused, as the
// settings form shows it.
func (a *API) addFeed(c *echo.Context) error {
	if err := a.available(); err != nil {
		return err
	}
	var in feedInput
	if err := readInput(c, &in, 4<<10); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := a.svc.AddFeed(ctx, auth.MustUser(ctx), strings.TrimSpace(in.URL)); err != nil {
		return httpx.Fail(err, "That feed could not be added.")
	}
	return a.writeSettings(c, http.StatusCreated)
}

// removeFeed treats a feed that is already gone as removed.
func (a *API) removeFeed(c *echo.Context) error {
	if err := a.available(); err != nil {
		return err
	}
	id, err := echox.UUIDParam(c, "feedID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.RemoveFeed(ctx, auth.MustUser(ctx), id); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		return httpx.Fail(err, "That feed could not be removed.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) writeSettings(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	enabled, err := a.svc.Enabled(ctx, user)
	if err != nil {
		return httpx.Fail(err, "The news settings could not be loaded.")
	}
	feeds, err := a.svc.Feeds(ctx, user)
	if err != nil {
		return httpx.Fail(err, "The news settings could not be loaded.")
	}
	out := NewsSettings{Enabled: enabled, Feeds: make([]FeedView, 0, len(feeds)), MaxFeeds: a.svc.MaxUserFeeds}
	for _, f := range feeds {
		out.Feeds = append(out.Feeds, FeedView{ID: f.ID, Title: f.Title, URL: f.URL})
	}
	return c.JSON(status, out)
}

// readInput decodes a settings body. Any body it cannot read is a validation
// failure, whatever the reason, so the reply is the same 422 either way.
func readInput(c *echo.Context, dst any, maxBytes int64) error {
	if err := httpx.ReadJSON(c.Response(), c.Request(), dst, httpx.ReadOptions{MaxBytes: maxBytes}); err != nil {
		return httpx.Fail(apperr.ErrValidation, "That request could not be read.")
	}
	return nil
}

// available answers 404 on a deployment without feeds configured.
func (a *API) available() error {
	if a.svc == nil {
		return httpx.Fail(apperr.ErrNotFound, "News is not available here.")
	}
	return nil
}
