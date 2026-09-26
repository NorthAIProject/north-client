package caffeine

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Handler takes caffeine from My Day's quick-add. It renders no page of its
// own: every write lands back on the day.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Post("/caffeine", h.log)
}

func (h *Handler) log(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r.Context(), "day.error"), http.StatusBadRequest)
		return
	}
	mg, _ := strconv.Atoi(r.PostFormValue("mg"))
	in := LogInput{Preset: r.PostFormValue("preset"), MG: mg, Label: r.PostFormValue("label")}
	if _, err := h.svc.Log(r.Context(), auth.MustUser(r.Context()), in); err != nil {
		middleware.FromContext(r.Context()).Warn("log caffeine", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "day.log.invalid"), http.StatusUnprocessableEntity)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}
