package lighterday

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	lighterpages "github.com/NorthAIProject/north-client/web/lighterday"
)

// Handler serves the offer as an HTMX fragment for the dashboard.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/today/lighter", h.show)
	r.Post("/today/lighter", h.choose)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	t, err := h.svc.Today(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		// The dashboard must not lose its training panel over an offer.
		middleware.FromContext(r.Context()).Warn("lighter day unavailable", slog.Any("error", err))
		t = Today{}
	}
	h.render(w, r, t)
}

func (h *Handler) choose(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
		return
	}
	t, err := h.svc.Choose(r.Context(), auth.MustUser(r.Context()), r.PostFormValue("choice"))
	if err != nil {
		if apperr.Is(err, apperr.ErrValidation) {
			http.Error(w, "That could not be saved.", http.StatusUnprocessableEntity)
			return
		}
		middleware.FromContext(r.Context()).Error("lighter day failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	// Without HTMX the form posted the whole page: go back to it.
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	h.render(w, r, t)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, t Today) {
	rd := t.Readiness
	card := lighterpages.Card{
		Offered: t.Offered(), Session: t.Session, Choice: t.Choice,
		Why: lighterpages.WhyLine(rd.HRV, rd.HRVBaseline, rd.HasHRV, rd.RHR, rd.RHRBaseline, rd.HasRHR),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := lighterpages.Fragment(card).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}
