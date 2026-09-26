package soreness

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Handler takes soreness from My Day's body panel.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Post("/soreness", h.set)
}

// set takes a region and a severity; severity 0 clears the region.
func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r.Context(), "day.error"), http.StatusBadRequest)
		return
	}
	user := auth.MustUser(r.Context())
	region := r.PostFormValue("region")
	severity, _ := strconv.Atoi(r.PostFormValue("severity"))
	var err error
	if severity == 0 {
		err = h.svc.Clear(r.Context(), user, region)
	} else {
		_, err = h.svc.Set(r.Context(), user, region, severity, r.PostFormValue("note"))
	}
	if err != nil {
		middleware.FromContext(r.Context()).Warn("set soreness", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "day.log.invalid"), http.StatusUnprocessableEntity)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}
