package soreness

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
)

// API is today's soreness, and its recent history, for native clients.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/soreness", a.today)
	g.GET("/soreness/history", a.history)
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
	// Figure says which regions of the body figure (GET /body/map ids) each
	// soreness region covers, so a client can paint soreness on the figure
	// and turn a tap on a muscle into a region. Knees and feet cover none.
	Figure map[string][]string `json:"figure"`
}

type SorenessDayEntryView struct {
	// Date is the calendar day, YYYY-MM-DD, in the user's timezone.
	Date     string `json:"date"`
	Region   string `json:"region"`
	Severity int    `json:"severity"`
	Note     string `json:"note,omitempty"`
}

type SorenessHistoryView struct {
	Days int `json:"days"`
	// To is the last day covered, the user's today; the first is Days-1 before it.
	To string `json:"to"`
	// Entries are newest first. A day with nothing sore has none.
	Entries []SorenessDayEntryView `json:"entries"`
}

// historyDefaultDays is how far back GET /soreness/history looks when not told.
const historyDefaultDays = 14

func (a *API) history(c *echo.Context) error {
	ctx := c.Request().Context()
	days := historyDefaultDays
	if raw := c.QueryParam("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			msg := "Days is a whole number."
			return httpx.Fail(apperr.FieldErrors{}.Add("days", msg), msg)
		}
		days = n
	}
	user := auth.MustUser(ctx)
	entries, err := a.svc.History(ctx, user, days)
	if err != nil {
		return httpx.Fail(err, "The soreness history could not be loaded.")
	}
	return c.JSON(http.StatusOK, ProjectHistory(days, a.svc.Today(user), entries))
}

// ProjectHistory is the history payload.
func ProjectHistory(days int, today time.Time, entries []Entry) SorenessHistoryView {
	out := SorenessHistoryView{Days: days, To: today.Format(time.DateOnly), Entries: make([]SorenessDayEntryView, len(entries))}
	for i, e := range entries {
		out.Entries[i] = SorenessDayEntryView{Date: e.LogDate.Format(time.DateOnly), Region: e.Region, Severity: e.Severity, Note: e.Note}
	}
	return out
}

// figureRegions is each soreness region's regions of the body figure: its
// muscles, each on the region the figure draws it on, once.
func figureRegions() map[string][]string {
	out := make(map[string][]string, len(sore.Muscles))
	for region, muscles := range sore.Muscles {
		on := []string{}
		for _, m := range muscles {
			key, ok := bodymap.RegionOf(m)
			if !ok {
				continue
			}
			seen := false
			for _, have := range on {
				seen = seen || have == key
			}
			if !seen {
				on = append(on, key)
			}
		}
		out[region] = on
	}
	return out
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
	out := SorenessTodayView{Entries: make([]SorenessEntryView, len(entries)), Regions: sore.Regions(), Figure: figureRegions()}
	for i, e := range entries {
		out.Entries[i] = SorenessEntryView{Region: e.Region, Severity: e.Severity, Note: e.Note}
	}
	return out
}
