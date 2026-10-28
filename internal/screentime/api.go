package screentime

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API takes a day's screen time from the app or from a Shortcut.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.PUT("/screen-time", a.set)
}

type ScreenTimeRequest struct {
	// Date is YYYY-MM-DD; today when absent.
	Date    string `json:"date,omitempty"`
	Minutes int    `json:"minutes"`
	// Source is manual or shortcut.
	Source string `json:"source,omitempty"`
}

type ScreenTimeView struct {
	Date    string `json:"date"`
	Minutes int    `json:"minutes"`
	Source  string `json:"source"`
}

func (a *API) set(c *echo.Context) error {
	var req ScreenTimeRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var date *time.Time
	if req.Date != "" {
		d, err := time.ParseInLocation("2006-01-02", req.Date, user.Location())
		if err != nil {
			return httpx.Fail(err, "Date must be YYYY-MM-DD.")
		}
		date = &d
	}
	day, err := a.svc.Set(ctx, user, date, req.Minutes, req.Source)
	if err != nil {
		return httpx.Fail(err, "The screen time could not be saved.")
	}
	return c.JSON(http.StatusOK, ScreenTimeView{Date: day.LocalDate.Format("2006-01-02"), Minutes: day.Minutes, Source: day.Source})
}
