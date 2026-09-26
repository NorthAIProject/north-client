package screentime

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Handler takes screen time from My Day's quick-add.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Post("/screen-time", h.set)
}

func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r.Context(), "day.error"), http.StatusBadRequest)
		return
	}
	hours, _ := strconv.Atoi(r.PostFormValue("hours"))
	minutes, _ := strconv.Atoi(r.PostFormValue("minutes"))
	if _, err := h.svc.Set(r.Context(), auth.MustUser(r.Context()), nil, hours*60+minutes, "manual"); err != nil {
		middleware.FromContext(r.Context()).Warn("set screen time", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "day.log.invalid"), http.StatusUnprocessableEntity)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}
