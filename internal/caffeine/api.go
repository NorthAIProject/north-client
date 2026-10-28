package caffeine

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/caffeine/caffeine"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is caffeine for native clients. Every change answers with today's
// drinks, so a client never adds anything up itself.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/caffeine", a.today)
	g.POST("/caffeine", a.log)
	g.DELETE("/caffeine/:entryID", a.undo)
}

type CaffeineRequest struct {
	// Preset is espresso, coffee, tea, energy_drink or cola; it fills mg and
	// label when they are left out.
	Preset   string     `json:"preset,omitempty"`
	MG       int        `json:"mg,omitempty"`
	Label    string     `json:"label,omitempty"`
	LoggedAt *time.Time `json:"loggedAt,omitempty"`
}

type CaffeineEntryView struct {
	ID       uuid.UUID `json:"id"`
	MG       int       `json:"mg"`
	Label    string    `json:"label"`
	LoggedAt time.Time `json:"loggedAt"`
}

type CaffeineTodayView struct {
	TotalMG  int                 `json:"totalMg"`
	ActiveMG int                 `json:"activeMg"`
	LimitMG  int                 `json:"limitMg"`
	Entries  []CaffeineEntryView `json:"entries"`
}

func (a *API) today(c *echo.Context) error { return a.respond(c, http.StatusOK) }

func (a *API) log(c *echo.Context) error {
	var req CaffeineRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	in := LogInput{Preset: req.Preset, MG: req.MG, Label: req.Label, At: req.LoggedAt}
	if _, err := a.svc.Log(ctx, auth.MustUser(ctx), in); err != nil {
		return httpx.Fail(err, "The caffeine could not be logged.")
	}
	return a.respond(c, http.StatusCreated)
}

func (a *API) undo(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("entryID"))
	if err != nil {
		return httpx.Fail(apperr.ErrNotFound, "No such drink.")
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
		return httpx.Fail(err, "Today's caffeine could not be loaded.")
	}
	return c.JSON(status, ProjectToday(entries, time.Now()))
}

// ProjectToday is the today payload.
func ProjectToday(entries []Entry, now time.Time) CaffeineTodayView {
	out := CaffeineTodayView{
		TotalMG:  caffeine.Total(entries),
		ActiveMG: int(caffeine.Active(entries, now)),
		LimitMG:  caffeine.DailyLimitMG,
		Entries:  make([]CaffeineEntryView, len(entries)),
	}
	for i, e := range entries {
		out.Entries[i] = CaffeineEntryView{ID: e.ID, MG: e.MG, Label: e.Label, LoggedAt: e.LoggedAt}
	}
	return out
}
