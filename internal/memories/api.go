package memories

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is what the coach remembers, for native clients: the memories it has
// proposed and is waiting for a verdict on, the ones it uses, and the
// controls the web /memories page has over each.
type API struct {
	svc *Service
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/memories", a.list)
	g.GET("/memories/for/:conversationID", a.forConversation)
	g.POST("/memories", a.create)
	g.PUT("/memories/:memoryID", a.update)
	g.POST("/memories/:memoryID/approve", a.approve)
	g.POST("/memories/:memoryID/reject", a.reject)
	g.PUT("/memories/:memoryID/pinned", a.pin)
	g.PUT("/memories/:memoryID/excluded", a.exclude)
	g.DELETE("/memories/:memoryID", a.destroy)
}

type MemoryView struct {
	ID       uuid.UUID `json:"id"`
	Category string    `json:"category"`
	Content  string    `json:"content"`
	// Status is pending (proposed by the coach), approved or rejected.
	Status string `json:"status"`
	// Pinned memories always reach the coach; excluded ones never do. The
	// two are exclusive: setting either clears the other.
	Pinned   bool `json:"pinned"`
	Excluded bool `json:"excluded"`
	// Source is how it was learned: written by hand, or extracted from a
	// conversation.
	Source               string     `json:"source"`
	SourceConversationID *uuid.UUID `json:"sourceConversationId,omitempty"`
	// SupersedesID is set on a proposal that would replace an existing
	// fact; approving it retires that fact.
	SupersedesID *uuid.UUID `json:"supersedesId,omitempty"`
	// ValidTo is set once a newer fact has replaced this one. A retired
	// fact never reaches the coach, whatever pinned or excluded say.
	ValidTo   *time.Time `json:"validTo,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type MemoryList struct {
	Pending    []MemoryView `json:"pending"`
	Approved   []MemoryView `json:"approved"`
	Categories []string     `json:"categories"`
}

// ConversationMemories is what the coach proposed to remember from one
// conversation and is still waiting for a verdict on.
type ConversationMemories struct {
	Pending []MemoryView `json:"pending"`
}

type MemoryRequest struct {
	Category string `json:"category"`
	Content  string `json:"content"`
}

type ToggleRequest struct {
	Value bool `json:"value"`
}

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	pending, err := a.svc.ListPending(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "Memories could not be loaded.")
	}
	approved, err := a.svc.ListApproved(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "Memories could not be loaded.")
	}
	return c.JSON(http.StatusOK, MemoryList{Pending: project(pending), Approved: project(approved), Categories: Categories})
}

func (a *API) forConversation(c *echo.Context) error {
	conversationID, err := echox.UUIDParam(c, "conversationID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	pending, err := a.svc.ListPendingForConversation(ctx, auth.MustUser(ctx).ID, conversationID)
	if err != nil {
		return httpx.Fail(err, "Memories could not be loaded.")
	}
	return c.JSON(http.StatusOK, ConversationMemories{Pending: project(pending)})
}

func (a *API) create(c *echo.Context) error {
	var req MemoryRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	m, err := a.svc.Create(ctx, auth.MustUser(ctx).ID, Input(req))
	return respond(c, http.StatusCreated, m, err)
}

func (a *API) update(c *echo.Context) error {
	var req MemoryRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	return a.change(c, func(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
		return a.svc.Update(ctx, id, userID, Input(req))
	})
}

func (a *API) approve(c *echo.Context) error { return a.change(c, a.svc.Approve) }
func (a *API) reject(c *echo.Context) error  { return a.change(c, a.svc.Reject) }

func (a *API) pin(c *echo.Context) error {
	var req ToggleRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	return a.change(c, func(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
		return a.svc.SetPinned(ctx, id, userID, req.Value)
	})
}

func (a *API) exclude(c *echo.Context) error {
	var req ToggleRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	return a.change(c, func(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
		return a.svc.SetExcluded(ctx, id, userID, req.Value)
	})
}

func (a *API) destroy(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "memoryID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.Delete(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The memory could not be deleted.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) change(c *echo.Context, op func(context.Context, uuid.UUID, uuid.UUID) (Memory, error)) error {
	id, err := echox.UUIDParam(c, "memoryID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	m, err := op(ctx, id, auth.MustUser(ctx).ID)
	return respond(c, http.StatusOK, m, err)
}

func respond(c *echo.Context, status int, m Memory, err error) error {
	if err != nil {
		return httpx.Fail(err, "The memory could not be saved.")
	}
	return c.JSON(status, projectOne(m))
}

func project(list []Memory) []MemoryView {
	out := make([]MemoryView, 0, len(list))
	for _, m := range list {
		out = append(out, projectOne(m))
	}
	return out
}

func projectOne(m Memory) MemoryView {
	return MemoryView{
		ID: m.ID, Category: m.Category, Content: m.Content, Status: m.Status,
		Pinned: m.Pinned, Excluded: m.Excluded, Source: m.Source,
		SourceConversationID: m.SourceConversationID, SupersedesID: m.SupersedesID,
		ValidTo: m.ValidTo, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func readJSON(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 16 << 10}, "The request body could not be read.")
}
