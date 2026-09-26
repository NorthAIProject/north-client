package milestones

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Handler manages trackers from My Day's vitals strip.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Post("/trackers", h.create)
	r.Post("/trackers/{id}/done", h.done)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r.Context(), "day.error"), http.StatusBadRequest)
		return
	}
	user := auth.MustUser(r.Context())
	var last *time.Time
	if v := r.PostFormValue("last_done_on"); v != "" {
		if d, err := time.ParseInLocation("2006-01-02", v, user.Location()); err == nil {
			last = &d
		}
	}
	var interval *int
	if n, err := strconv.Atoi(r.PostFormValue("interval_months")); err == nil && n > 0 {
		interval = &n
	}
	if _, err := h.svc.Create(r.Context(), user, r.PostFormValue("name"), last, interval); err != nil {
		middleware.FromContext(r.Context()).Warn("create milestone", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "day.log.invalid"), http.StatusUnprocessableEntity)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (h *Handler) done(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err == nil {
		_, err = h.svc.Done(r.Context(), auth.MustUser(r.Context()), id)
	}
	if err != nil {
		middleware.FromContext(r.Context()).Warn("milestone done", slog.Any("error", err))
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}
