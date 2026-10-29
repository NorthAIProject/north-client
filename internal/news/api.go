package news

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the news ticker for native clients: the latest items from the feeds
// the person follows. Feeds are chosen on the web.
type API struct {
	svc *Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/news", a.ticker)
	a.registerSettings(g)
}

type NewsItemView struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"publishedAt"`
}

type NewsTicker struct {
	// Enabled is false when the person turned the ticker off.
	Enabled bool           `json:"enabled"`
	Items   []NewsItemView `json:"items"`
}

func (a *API) ticker(c *echo.Context) error {
	// A deployment without feeds configured has no news service.
	if a.svc == nil {
		return c.JSON(http.StatusOK, NewsTicker{Items: []NewsItemView{}})
	}
	ctx := c.Request().Context()
	t, err := a.svc.Ticker(ctx, auth.MustUser(ctx), 20)
	if err != nil {
		return httpx.Fail(err, "The news could not be loaded.")
	}
	out := NewsTicker{Enabled: t.Enabled, Items: make([]NewsItemView, 0, len(t.Items))}
	for _, it := range t.Items {
		out.Items = append(out.Items, NewsItemView{Title: it.Title, URL: it.URL, Source: it.Source, PublishedAt: it.PublishedAt})
	}
	return c.JSON(http.StatusOK, out)
}
