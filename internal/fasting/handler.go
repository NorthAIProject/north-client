package fasting

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Handler starts and ends fasts from My Day's fasting card.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Post("/fasting/start", h.start)
	r.Post("/fasting/stop", h.stop)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r.Context(), "day.error"), http.StatusBadRequest)
		return
	}
	target, _ := strconv.Atoi(r.PostFormValue("target_hours"))
	if _, err := h.svc.Start(r.Context(), auth.MustUser(r.Context()), target, nil); err != nil {
		middleware.FromContext(r.Context()).Warn("start fast", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "day.log.invalid"), http.StatusUnprocessableEntity)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	if _, err := h.svc.Stop(r.Context(), auth.MustUser(r.Context())); err != nil {
		middleware.FromContext(r.Context()).Warn("stop fast", slog.Any("error", err))
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}
