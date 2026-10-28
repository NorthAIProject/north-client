package checkinreply

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the note under today's check-in for native clients. Register
// behind auth.Bearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

// Register adds the one endpoint. POST because asking may write the note:
// it is not safe to repeat for free the first time.
func (a *API) Register(g *echo.Group) {
	g.POST("/check-ins/today/reply", a.reply)
}

// ReplyView is one note.
type ReplyView struct {
	// Kind is daily or pattern. Open-ended; treat a value you do not know as
	// daily.
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func (a *API) reply(c *echo.Context) error {
	ctx := c.Request().Context()
	reply, ok, err := a.svc.ForToday(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, "The note could not be loaded.")
	}
	if !ok {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, ReplyView(reply))
}
