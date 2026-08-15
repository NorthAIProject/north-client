package nudges

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/htmx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	nudgepages "github.com/NorthAIProject/north-client/web/nudges"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the bell and the read/dismiss actions. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/nudges/bell", h.bell)
	r.Post("/nudges/{id}/read", h.read)
	r.Post("/nudges/{id}/dismiss", h.dismiss)
}

func (h *Handler) bell(w http.ResponseWriter, r *http.Request) {
	h.renderBell(w, r)
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	n, err := h.svc.MarkRead(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	if htmx.IsRequest(r) {
		h.renderBell(w, r)
		return
	}

	dest := n.Href
	if dest == "" {
		dest = "/app"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (h *Handler) dismiss(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if _, err := h.svc.Dismiss(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	if htmx.IsRequest(r) {
		h.renderBell(w, r)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (h *Handler) renderBell(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	list, err := h.svc.ListOpen(r.Context(), user.ID, listDefault)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	unread, err := h.svc.CountUnread(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := nudgepages.Bell(list, unread).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render nudge bell", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	default:
		middleware.FromContext(r.Context()).Error("nudge request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
