package vault

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	vaultpages "github.com/NorthAIProject/north-client/web/settings/vault"
)

type Handler struct {
	svc     *Service
	enabled bool
}

// NewHandler builds the vault handler. enabled is false in a hosted deployment:
// Connect stats a path on the server's own filesystem and the sync job walks it,
// so in a container the folder a user types resolves inside the pod and the
// feature cannot work. Nothing links to these routes, so switching them off
// removes a guaranteed failure rather than a capability.
func NewHandler(svc *Service, enabled bool) *Handler {
	return &Handler{svc: svc, enabled: enabled}
}

func (h *Handler) Routes(r chi.Router) {
	if !h.enabled {
		return
	}
	r.Get("/settings/vault", h.show)
	r.Post("/settings/vault/connect", h.connect)
	r.Post("/settings/vault/disconnect", h.disconnect)
	r.Post("/settings/vault/sync", h.sync)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	conn, err := h.svc.Get(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, vaultpages.Page(user, toView(conn), vaultpages.Form{}))
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	form := vaultpages.Form{RootPath: r.PostFormValue("root_path")}
	if _, err := h.svc.Connect(r.Context(), user.ID, form.RootPath); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			conn, _ := h.svc.Get(r.Context(), user.ID)
			render(w, r, http.StatusUnprocessableEntity, vaultpages.Page(user, toView(conn), form))
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/settings/vault", http.StatusSeeOther)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := h.svc.Disconnect(r.Context(), user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/settings/vault", http.StatusSeeOther)
}

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := h.svc.SyncNow(r.Context(), user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/settings/vault", http.StatusSeeOther)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("vault request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

func toView(conn Connection) vaultpages.View {
	return vaultpages.View{
		RootPath:   conn.RootPath,
		LastSyncAt: conn.LastSyncAt,
		LastError:  conn.LastError,
	}
}
