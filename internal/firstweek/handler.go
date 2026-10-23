package firstweek

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/moments"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	firstweekpages "github.com/NorthAIProject/north-client/web/firstweek"
)

// Handler is the week's day card, as a fragment on My Day and the dashboard.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/first-week", h.show)
	r.Post("/first-week/done", h.done)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	t, ok, err := h.svc.Today(r.Context(), user)
	if err != nil {
		// The page must not lose itself over the card.
		middleware.FromContext(r.Context()).Warn("first week unavailable", slog.Any("error", err))
		ok = false
	}
	h.render(w, r, t, ok, false)
}

func (h *Handler) done(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	before, _, err := h.svc.Today(r.Context(), user)
	if err == nil {
		_, err = h.svc.MarkDone(r.Context(), user)
	}
	if err != nil && !apperr.Is(err, apperr.ErrConflict) && !apperr.Is(err, apperr.ErrNotFound) {
		middleware.FromContext(r.Context()).Error("first week done failed", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "outreach.error"), http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	// A conflict is a stale card (the week ended, or the day is over): show
	// what is true now.
	t, ok, err := h.svc.Today(r.Context(), user)
	if err != nil {
		middleware.FromContext(r.Context()).Error("first week reload failed", slog.Any("error", err))
		ok = false
	}
	// The moment is counted once, on the tap that earned it; the card keeps
	// showing it for the rest of day 3 without counting it again.
	h.render(w, r, t, ok, t.ThreeOfThree() && !before.ThreeOfThree())
}

// render shows the card for a running week on days 0 to 7 and nothing
// otherwise: once the week is over, the card has nothing to ask.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, t Today, ok, earned bool) {
	c := firstweekpages.Card{Show: ok && t.Programme.Status == StatusActive && t.Day >= 0 && t.Day <= Days}
	if c.Show {
		c.Day, c.Done = t.Day, t.Done
		c.Action = i18n.T(r.Context(), "firstweek."+t.Programme.Template+".action")
		c.Days = make([]bool, len(t.Days))
		for i, d := range t.Days {
			c.Days[i] = d.Done
		}
		if t.ThreeOfThree() {
			m := moments.ForFirstWeekThree(r.Context())
			c.Moment = &m
			if earned {
				h.svc.funnel.MomentShown(r.Context(), t.Programme.UserID, m.Kind)
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := firstweekpages.Fragment(c).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render first week", slog.Any("error", err))
	}
}
