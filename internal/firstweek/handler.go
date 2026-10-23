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
	r.Get("/first-week/review", h.review)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	t, ok, err := h.svc.Today(r.Context(), user)
	if err != nil {
		// The page must not lose itself over the card.
		middleware.FromContext(r.Context()).Warn("first week unavailable", slog.Any("error", err))
		ok = false
	}
	h.render(w, r, t, ok)
}

func (h *Handler) done(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	_, err := h.svc.TapDone(r.Context(), user)
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
	h.render(w, r, t, ok)
}

// render shows the card for a running week from day 0 until the review is
// opened (or the week is abandoned), and nothing otherwise.
// The "3 of 3 days" moment is counted where it is earned (Service.complete),
// so the card shows it for the rest of day 3 without counting it again.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, t Today, ok bool) {
	c := firstweekpages.Card{Show: ok && t.Programme.Status == StatusActive && t.Day >= 0}
	if c.Show {
		c.Day, c.Done = t.Day, t.Done
		c.ReviewReady, c.Over = t.Day >= Days, t.Day > Days
		c.Action = i18n.T(r.Context(), "firstweek."+t.Programme.Template+".action")
		c.Days = make([]bool, len(t.Days))
		for i, d := range t.Days {
			c.Days[i] = d.Done
		}
		if t.ThreeOfThree() {
			m := moments.ForFirstWeekThree(r.Context())
			c.Moment = &m
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := firstweekpages.Fragment(c).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render first week", slog.Any("error", err))
	}
}

// review is the day-7 review. Opening it finishes the week; before day 7,
// or for a week that has none, it is back to My Day.
func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	t, rv, err := h.svc.OpenReview(r.Context(), user)
	if apperr.Is(err, apperr.ErrConflict) || apperr.Is(err, apperr.ErrNotFound) {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	if err != nil {
		middleware.FromContext(r.Context()).Error("first week review failed", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "outreach.error"), http.StatusInternalServerError)
		return
	}
	page := firstweekpages.Review{
		Text: rv.Text, DaysDone: rv.DaysDone, Days: make([]bool, len(t.Days)),
		Label: i18n.T(r.Context(), "firstweek."+t.Programme.Template+".label"),
	}
	for i, d := range t.Days {
		page.Days[i] = d.Done
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := firstweekpages.ReviewPage(user, page).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render first week review", slog.Any("error", err))
	}
}
