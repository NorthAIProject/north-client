package health

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// WebHandler takes a blood pressure reading typed into My Day's body panel.
// Separate from Handler, which is the bridge ingest endpoint and knows nothing
// of sessions or forms.
type WebHandler struct {
	svc *Service
}

func NewWebHandler(svc *Service) *WebHandler { return &WebHandler{svc: svc} }

func (h *WebHandler) Routes(r chi.Router) {
	r.Post("/blood-pressure", h.bloodPressure)
}

func (h *WebHandler) bloodPressure(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r.Context(), "day.error"), http.StatusBadRequest)
		return
	}
	sys, _ := strconv.Atoi(r.PostFormValue("systolic"))
	dia, _ := strconv.Atoi(r.PostFormValue("diastolic"))
	if _, err := h.svc.RecordBloodPressure(r.Context(), auth.MustUser(r.Context()).ID, sys, dia, nil); err != nil {
		middleware.FromContext(r.Context()).Warn("record blood pressure", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "day.log.invalid"), http.StatusUnprocessableEntity)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}
