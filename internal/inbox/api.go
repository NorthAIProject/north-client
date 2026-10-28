package inbox

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/inbox/item"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the capture inbox for native clients and the share extension.
// Register behind auth.Bearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/inbox", a.list)
	g.POST("/inbox", a.add)
	g.POST("/inbox/:itemID/file", a.file)
	g.POST("/inbox/:itemID/dismiss", a.dismiss)
}

type SuggestionView struct {
	// Destination is goal_note, knowledge or journal.
	Destination string     `json:"destination"`
	GoalID      *uuid.UUID `json:"goalId,omitempty"`
	GoalTitle   string     `json:"goalTitle,omitempty"`
	// Title is a suggested knowledge-note title.
	Title string `json:"title,omitempty"`
	Why   string `json:"why"`
}

type ItemView struct {
	ID     uuid.UUID `json:"id"`
	Text   string    `json:"text"`
	Source string    `json:"source"`
	// Suggestion is absent while the coach has not looked at it yet.
	Suggestion *SuggestionView `json:"suggestion,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type InboxView struct {
	Items []ItemView `json:"items"`
	// Open is how many are waiting, which can be more than items lists.
	Open int `json:"open"`
}

type AddRequest struct {
	Text string `json:"text"`
	// Source is app, share, shortcut or web; app when absent.
	Source string `json:"source"`
}

type FileRequest struct {
	Destination string    `json:"destination"`
	GoalID      uuid.UUID `json:"goalId"`
	Title       string    `json:"title"`
}

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	items, open, err := a.svc.Open(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The inbox could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectInbox(items, open))
}

func (a *API) add(c *echo.Context) error {
	var req AddRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	if req.Source == "" {
		req.Source = item.SourceApp
	}
	ctx := c.Request().Context()
	it, err := a.svc.Add(ctx, auth.MustUser(ctx).ID, req.Source, req.Text)
	if err != nil {
		return httpx.Fail(err, "That could not be saved.")
	}
	return c.JSON(http.StatusCreated, projectItem(it))
}

func (a *API) file(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "itemID")
	if err != nil {
		return err
	}
	var req FileRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	it, err := a.svc.File(ctx, auth.MustUser(ctx).ID, id, Filing(req))
	if err != nil {
		return httpx.Fail(err, "That could not be filed.")
	}
	return c.JSON(http.StatusOK, projectItem(it))
}

func (a *API) dismiss(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "itemID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.Dismiss(ctx, auth.MustUser(ctx).ID, id); err != nil {
		return httpx.Fail(err, "That could not be dismissed.")
	}
	return c.NoContent(http.StatusNoContent)
}

func projectInbox(items []Item, open int) InboxView {
	out := InboxView{Items: make([]ItemView, 0, len(items)), Open: open}
	for _, it := range items {
		out.Items = append(out.Items, projectItem(it))
	}
	return out
}

func projectItem(it Item) ItemView {
	v := ItemView{ID: it.ID, Text: it.Text, Source: it.Source, CreatedAt: it.CreatedAt}
	if s := it.Suggestion; s != nil {
		v.Suggestion = &SuggestionView{Destination: s.Destination, GoalID: s.GoalID, GoalTitle: s.GoalTitle, Title: s.Title, Why: s.Why}
	}
	return v
}

func readJSON(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 32 << 10}, "The request body could not be read.")
}
