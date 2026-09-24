package memories

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is what the coach remembers, for native clients: the memories it has
// proposed and is waiting for a verdict on, the ones it uses, and the
// controls the web /memories page has over each.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/memories", a.list)
	r.Post("/memories", a.create)
	r.Put("/memories/{memoryID}", a.update)
	r.Post("/memories/{memoryID}/approve", a.approve)
	r.Post("/memories/{memoryID}/reject", a.reject)
	r.Put("/memories/{memoryID}/pinned", a.pin)
	r.Put("/memories/{memoryID}/excluded", a.exclude)
	r.Delete("/memories/{memoryID}", a.destroy)
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
	CreatedAt            time.Time  `json:"createdAt"`
}

type MemoryList struct {
	Pending    []MemoryView `json:"pending"`
	Approved   []MemoryView `json:"approved"`
	Categories []string     `json:"categories"`
}

type MemoryRequest struct {
	Category string `json:"category"`
	Content  string `json:"content"`
}

type ToggleRequest struct {
	Value bool `json:"value"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUser(r.Context()).ID
	pending, err := a.svc.ListPending(r.Context(), userID)
	if err != nil {
		httpx.Error(w, err, "Memories could not be loaded.")
		return
	}
	approved, err := a.svc.ListApproved(r.Context(), userID)
	if err != nil {
		httpx.Error(w, err, "Memories could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, MemoryList{Pending: project(pending), Approved: project(approved), Categories: Categories})
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req MemoryRequest
	if !readJSON(w, r, &req) {
		return
	}
	m, err := a.svc.Create(r.Context(), auth.MustUser(r.Context()).ID, Input(req))
	a.respond(w, http.StatusCreated, m, err)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	var req MemoryRequest
	if !readJSON(w, r, &req) {
		return
	}
	a.change(w, r, func(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
		return a.svc.Update(ctx, id, userID, Input(req))
	})
}

func (a *API) approve(w http.ResponseWriter, r *http.Request) { a.change(w, r, a.svc.Approve) }
func (a *API) reject(w http.ResponseWriter, r *http.Request)  { a.change(w, r, a.svc.Reject) }

func (a *API) pin(w http.ResponseWriter, r *http.Request) {
	var req ToggleRequest
	if !readJSON(w, r, &req) {
		return
	}
	a.change(w, r, func(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
		return a.svc.SetPinned(ctx, id, userID, req.Value)
	})
}

func (a *API) exclude(w http.ResponseWriter, r *http.Request) {
	var req ToggleRequest
	if !readJSON(w, r, &req) {
		return
	}
	a.change(w, r, func(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
		return a.svc.SetExcluded(ctx, id, userID, req.Value)
	})
}

func (a *API) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := memoryID(w, r)
	if !ok {
		return
	}
	if err := a.svc.Delete(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The memory could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) change(w http.ResponseWriter, r *http.Request, op func(context.Context, uuid.UUID, uuid.UUID) (Memory, error)) {
	id, ok := memoryID(w, r)
	if !ok {
		return
	}
	m, err := op(r.Context(), id, auth.MustUser(r.Context()).ID)
	a.respond(w, http.StatusOK, m, err)
}

func (a *API) respond(w http.ResponseWriter, status int, m Memory, err error) {
	if err != nil {
		httpx.Error(w, err, "The memory could not be saved.")
		return
	}
	httpx.WriteJSON(w, status, projectOne(m))
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
		SourceConversationID: m.SourceConversationID, CreatedAt: m.CreatedAt,
	}
}

func memoryID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "memoryID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
