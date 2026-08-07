package fitness

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	fitnesspages "github.com/NorthAIProject/north-client/web/fitness"
)

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/fitness", h.hub)
}

func (h *Handler) hub(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := fitnesspages.Hub(user).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render fitness hub", slog.Any("error", err))
	}
}
