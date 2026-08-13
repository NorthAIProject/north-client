package dashboard

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/web/app"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts the overview. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.show)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	snap, err := h.svc.Load(r.Context(), user)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	data := app.DashboardData{
		CheckedInToday:  snap.CheckedInToday,
		Streak:          snap.Streak,
		PendingMemories: snap.PendingMemories,
		Goals:           snap.Goals,
		LastThread:      snap.LastThread,
		NextSession:     snap.NextSession,
		PlanID:          snap.PlanID,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.Dashboard(user, data).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render dashboard", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("dashboard request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
