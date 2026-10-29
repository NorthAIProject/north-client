package mind

import (
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the journal for native clients: the web /mind page's entries, the
// mood trend beside them, and writing one.
type API struct {
	svc *Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/mind/journal", a.list)
	g.POST("/mind/journal", a.create)
}

type JournalEntryView struct {
	ID      uuid.UUID `json:"id"`
	Content string    `json:"content"`
	// Mood is 1-5 when given.
	Mood      *int      `json:"mood,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type MoodTrendView struct {
	AverageMood   float64 `json:"averageMood"`
	AverageEnergy float64 `json:"averageEnergy"`
	// Count is how many check-ins the averages come from.
	Count int `json:"count"`
}

type Journal struct {
	Entries []JournalEntryView `json:"entries"`
	Trend   MoodTrendView      `json:"trend"`
}

type JournalRequest struct {
	Content string `json:"content"`
	Mood    *int   `json:"mood,omitempty"`
}

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	entries, err := a.svc.Recent(ctx, userID, 50)
	if err != nil {
		return httpx.Fail(err, "The journal could not be loaded.")
	}
	trend, err := a.svc.RecentMoodTrend(ctx, userID, 14)
	if err != nil {
		return httpx.Fail(err, "The journal could not be loaded.")
	}
	out := Journal{
		Entries: make([]JournalEntryView, 0, len(entries)),
		Trend:   MoodTrendView(trend),
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, project(e))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) create(c *echo.Context) error {
	var req JournalRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 64 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	entry, err := a.svc.Create(ctx, auth.MustUser(ctx).ID, Input(req))
	if err != nil {
		return httpx.Fail(err, "The entry could not be saved.")
	}
	return c.JSON(http.StatusCreated, project(entry))
}

func project(e JournalEntry) JournalEntryView {
	out := JournalEntryView{ID: e.ID, Content: e.Content, CreatedAt: e.CreatedAt}
	if e.Mood != nil {
		out.Mood = util.Ptr(int(*e.Mood))
	}
	return out
}
