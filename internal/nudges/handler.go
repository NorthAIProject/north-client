package nudges

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
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

// Routes mounts the bell and the read/dismiss/open actions. Must be behind
// RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/nudges/bell", h.bell)
	r.Post("/nudges/{id}/read", h.read)
	r.Post("/nudges/{id}/dismiss", h.dismiss)
	r.Get("/nudges/{id}/open", h.open)
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

	n, err := h.svc.Open(r.Context(), id, user.ID, analytics.ChannelBell)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	if htmx.IsRequest(r) {
		h.renderBell(w, r)
		return
	}

	http.Redirect(w, r, destination(n), http.StatusSeeOther)
}

// open is the link a push notification carries. A GET because a notification
// tap can only navigate; the state it changes — read_at, and one funnel event
// — is idempotent, so a bookmark or a reload does no further harm. A nudge
// that was dismissed in the meantime sends the person home rather than to an
// error: they tapped something North sent them, and that should land somewhere.
func (h *Handler) open(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}

	n, err := h.svc.Open(r.Context(), id, user.ID, channelFrom(r))
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			http.Redirect(w, r, "/app", http.StatusSeeOther)
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, destination(n), http.StatusSeeOther)
}

// channelFrom reads which channel brought the person here. Only channels this
// build knows are accepted; anything else is counted as the bell rather than
// letting a query string invent a channel in the analytics.
func channelFrom(r *http.Request) string {
	switch r.URL.Query().Get("from") {
	case analytics.ChannelPush:
		return analytics.ChannelPush
	default:
		return analytics.ChannelBell
	}
}

// destination is where a nudge sends the person, or home for one without a
// page of its own.
func destination(n Nudge) string {
	if n.Href == "" {
		return "/app"
	}
	return n.Href
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
