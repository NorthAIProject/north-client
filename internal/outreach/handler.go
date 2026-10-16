package outreach

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	outreachpages "github.com/NorthAIProject/north-client/web/outreach"
)

// Handler is today's coach message as a fragment on the Today page.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/outreach", h.show)
	r.Post("/outreach/{id}/actions", h.act)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	o, ok, err := h.svc.Today(r.Context(), user)
	if err != nil {
		// Today must not lose its page over the coach's note.
		middleware.FromContext(r.Context()).Warn("outreach unavailable", slog.Any("error", err))
		ok = false
	}
	h.render(w, r, user, o, ok)
}

func (h *Handler) act(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	o, err := h.svc.Act(r.Context(), user, id, r.PostFormValue("action"))
	if err != nil {
		switch {
		case apperr.Is(err, apperr.ErrNotFound):
			http.NotFound(w, r)
			return
		case apperr.Is(err, apperr.ErrConflict), apperr.Is(err, apperr.ErrValidation):
			// Answered elsewhere already, or a stale card: show what is true now.
			o, _, _ = h.svc.Today(r.Context(), user)
		default:
			middleware.FromContext(r.Context()).Error("outreach answer failed", slog.Any("error", err))
			http.Error(w, i18n.T(r.Context(), "outreach.error"), http.StatusInternalServerError)
			return
		}
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	h.render(w, r, user, o, o.ID != uuid.Nil)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, _ users.User, o Outreach, ok bool) {
	c := outreachpages.Card{Show: ok && o.Body != ""}
	if c.Show {
		c.ID, c.Body = o.ID.String(), o.Body
		if o.Answered() {
			c.Outcome = Outcome(r.Context(), o.Action)
		} else {
			c.Actions = o.Actions()
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := outreachpages.Fragment(c).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render outreach", slog.Any("error", err))
	}
}
