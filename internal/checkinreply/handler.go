package checkinreply

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	checkinpages "github.com/NorthAIProject/north-client/web/checkins"
)

// Handler serves the note into the saved check-in panel on the web. Mount
// under /app behind the session.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/check-ins/today/reply", h.reply)
}

// reply always answers 200. htmx does not swap a 204, so "no note" is an
// empty body, which replaces the placeholder with nothing. A failure to read
// is logged and treated the same: the note is decoration.
func (h *Handler) reply(w http.ResponseWriter, r *http.Request) {
	reply, ok, err := h.svc.ForToday(r.Context(), auth.MustUser(r.Context()))
	if err != nil {
		middleware.FromContext(r.Context()).Warn("check-in reply unavailable", slog.Any("error", err))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if !ok {
		return
	}
	if err := checkinpages.ReplyNote(reply.Text, reply.Kind == KindPattern).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}
