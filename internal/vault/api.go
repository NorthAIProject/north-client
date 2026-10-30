package vault

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

type Handler struct {
	svc     *Service
	enabled bool
}

// NewHandler builds the vault handler. enabled is false in a hosted deployment:
// Connect stats a path on the server's own filesystem and the sync job walks it,
// so in a container the folder a user types resolves inside the pod and the
// feature cannot work. Nothing links to these routes, so switching them off
// removes a guaranteed failure rather than a capability.
func NewHandler(svc *Service, enabled bool) *Handler {
	return &Handler{svc: svc, enabled: enabled}
}

// RegisterAPI registers the vault for a web front end, on a group behind
// auth.Bearer.
//
// The same switch as Routes: a hosted deployment cannot read a folder on the
// person's own machine, so the routes do not exist there. A nil handler (a
// test's apiSet) registers nothing.
func (h *Handler) RegisterAPI(g *echo.Group) {
	if h == nil || !h.enabled {
		return
	}
	g.GET("/settings/vault", h.apiShow)
	g.PUT("/settings/vault", h.apiConnect)
	g.DELETE("/settings/vault", h.apiDisconnect)
	g.POST("/settings/vault/sync", h.apiSync)
}

// Status is the linked vault folder. RootPath is empty when none is linked.
type Status struct {
	RootPath   string     `json:"rootPath"`
	LastSyncAt *time.Time `json:"lastSyncAt,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
}

// ConnectRequest names the folder to link.
type ConnectRequest struct {
	RootPath string `json:"rootPath"`
}

func projectStatus(conn Connection) Status {
	return Status{RootPath: conn.RootPath, LastSyncAt: conn.LastSyncAt, LastError: conn.LastError}
}

func (h *Handler) apiShow(c *echo.Context) error {
	ctx := c.Request().Context()
	conn, err := h.svc.Get(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your vault could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectStatus(conn))
}

func (h *Handler) apiConnect(c *echo.Context) error {
	ctx := c.Request().Context()
	var req ConnectRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 16 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	conn, err := h.svc.Connect(ctx, auth.MustUser(ctx).ID, req.RootPath)
	if err != nil {
		return httpx.Fail(err, "That vault could not be connected.")
	}
	return c.JSON(http.StatusOK, projectStatus(conn))
}

func (h *Handler) apiDisconnect(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := h.svc.Disconnect(ctx, auth.MustUser(ctx).ID); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		return httpx.Fail(err, "Your vault could not be disconnected.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) apiSync(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := h.svc.SyncNow(ctx, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "Your vault could not be synced.")
	}
	return c.NoContent(http.StatusAccepted)
}
