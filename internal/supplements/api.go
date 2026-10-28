package supplements

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/supplements/supplement"
)

// API is supplements for native clients.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/supplements", a.today)
	g.POST("/supplements", a.log)
	g.DELETE("/supplements/:entryID", a.undo)
}

type SupplementRequest struct {
	Preset    string   `json:"preset,omitempty"`
	Name      string   `json:"name,omitempty"`
	Count     int      `json:"count,omitempty"`
	Nutrients []string `json:"nutrients,omitempty"`
}

type SupplementEntryView struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Count     int       `json:"count"`
	Nutrients []string  `json:"nutrients"`
	LoggedAt  time.Time `json:"loggedAt"`
}

type SupplementPresetView struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Nutrients []string `json:"nutrients"`
}

type SupplementsTodayView struct {
	Entries []SupplementEntryView  `json:"entries"`
	Presets []SupplementPresetView `json:"presets"`
	// Nutrients is the tracked set, in display order.
	Nutrients []string `json:"nutrients"`
}

func (a *API) today(c *echo.Context) error { return a.respond(c, http.StatusOK) }

func (a *API) log(c *echo.Context) error {
	var req SupplementRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := a.svc.Log(ctx, auth.MustUser(ctx), LogInput(req)); err != nil {
		return httpx.Fail(err, "The supplement could not be logged.")
	}
	return a.respond(c, http.StatusCreated)
}

func (a *API) undo(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("entryID"))
	if err != nil {
		return httpx.Fail(apperr.ErrNotFound, "No such entry.")
	}
	ctx := c.Request().Context()
	if err := a.svc.Undo(ctx, auth.MustUser(ctx), id); err != nil {
		return httpx.Fail(err, "That could not be undone.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) respond(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	entries, err := a.svc.Today(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, "Today's supplements could not be loaded.")
	}
	return c.JSON(status, ProjectToday(entries))
}

// ProjectToday is the today payload.
func ProjectToday(entries []Entry) SupplementsTodayView {
	out := SupplementsTodayView{Entries: make([]SupplementEntryView, len(entries)), Nutrients: supplement.Nutrients()}
	for i, e := range entries {
		out.Entries[i] = SupplementEntryView{ID: e.ID, Name: e.Name, Count: e.Count, Nutrients: append([]string{}, e.Nutrients...), LoggedAt: e.LoggedAt}
	}
	for _, p := range supplement.Presets() {
		out.Presets = append(out.Presets, SupplementPresetView{Key: p.Key, Name: p.Name, Nutrients: append([]string{}, p.Nutrients...)})
	}
	return out
}
