package export

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

type Handler struct {
	exporter *Exporter
}

func NewHandler(exporter *Exporter) *Handler { return &Handler{exporter: exporter} }

// Routes mounts the export. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/knowledge/export.zip", h.download)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="north-export.zip"`)

	// Streamed, so a long history never buffers. The response has begun by the
	// time anything can fail, which is why a mid-stream problem is logged and
	// written into the archive rather than turned into a status code that can
	// no longer be sent.
	if err := h.exporter.WriteZip(r.Context(), user, w); err != nil {
		middleware.FromContext(r.Context()).Error("export failed part-way",
			slog.Any("error", err), slog.String("user_id", user.ID.String()))
	}
}
