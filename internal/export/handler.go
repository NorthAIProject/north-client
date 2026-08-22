package export

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/account"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

type Handler struct {
	exporter *Exporter
	quotas   *quota.Service
	account  *account.Service
}

func NewHandler(exporter *Exporter, quotas *quota.Service, acct *account.Service) *Handler {
	return &Handler{exporter: exporter, quotas: quotas, account: acct}
}

// Routes mounts the export. Must be behind RequireAuth.
//
// Under /settings rather than /knowledge, where it started: it covers the whole
// account now, and a path that says knowledge would be describing a sixth of
// what comes out.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/settings/export.zip", h.download)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	// Before any header, because once the archive starts there is no status
	// code left to send. An export reads the entire account and pulls every
	// stored file back out of the bucket, which is worth bounding.
	if h.quotas != nil {
		decision, err := h.quotas.Consume(r.Context(), user.ID, string(user.Tier), quota.AccountExport)
		if err != nil {
			middleware.FromContext(r.Context()).Error("export quota check failed",
				slog.Any("error", err), slog.String("user_id", user.ID.String()))
		} else if !decision.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())))
			http.Error(w, "You have exported your data a few times just now. Try again shortly.",
				http.StatusTooManyRequests)
			return
		}
	}

	// Recorded before the first byte, for the same reason: a stream that fails
	// halfway cannot come back and tell anyone it happened.
	if h.account != nil {
		if err := h.account.RecordExport(r.Context(), user.ID); err != nil {
			middleware.FromContext(r.Context()).Error("could not record a data export",
				slog.Any("error", err), slog.String("user_id", user.ID.String()))
		}
	}

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
