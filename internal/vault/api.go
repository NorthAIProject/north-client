package vault

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// APIRoutes mounts the vault for a web front end, behind auth.RequireBearer.
//
// The same switch as Routes: a hosted deployment cannot read a folder on the
// person's own machine, so the routes do not exist there. A nil handler (a
// test's apiSet) mounts nothing.
func (h *Handler) APIRoutes(r chi.Router) {
	if h == nil || !h.enabled {
		return
	}
	r.Get("/settings/vault", h.apiShow)
	r.Put("/settings/vault", h.apiConnect)
	r.Delete("/settings/vault", h.apiDisconnect)
	r.Post("/settings/vault/sync", h.apiSync)
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

func (h *Handler) apiShow(w http.ResponseWriter, r *http.Request) {
	conn, err := h.svc.Get(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Your vault could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectStatus(conn))
}

func (h *Handler) apiConnect(w http.ResponseWriter, r *http.Request) {
	var req ConnectRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	conn, err := h.svc.Connect(r.Context(), auth.MustUser(r.Context()).ID, req.RootPath)
	if err != nil {
		httpx.Error(w, err, "That vault could not be connected.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectStatus(conn))
}

func (h *Handler) apiDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Disconnect(r.Context(), auth.MustUser(r.Context()).ID); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		httpx.Error(w, err, "Your vault could not be disconnected.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) apiSync(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.SyncNow(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Your vault could not be synced.")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
