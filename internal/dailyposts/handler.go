package dailyposts

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	dailypages "github.com/NorthAIProject/north-client/web/dailyposts"
)

// Handler serves /app/daily.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/daily", h.page)
	r.Post("/daily/{postID}/kudos", h.kudos(true))
	r.Post("/daily/{postID}/kudos/undo", h.kudos(false))
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	f, err := h.svc.Feed(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dailypages.Page(user, dailypages.View{Today: f.Today, Earlier: f.Earlier}).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render daily post", slog.Any("error", err))
	}
}

func (h *Handler) kudos(give bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "postID"))
		if err != nil {
			h.fail(w, r, apperr.ErrNotFound)
			return
		}
		viewer := auth.MustUser(r.Context()).ID
		if give {
			err = h.svc.GiveKudos(r.Context(), id, viewer)
		} else {
			err = h.svc.TakeKudos(r.Context(), id, viewer)
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
		http.Redirect(w, r, "/app/daily#post-"+id.String(), http.StatusSeeOther)
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	middleware.FromContext(r.Context()).Error("daily post failed", slog.Any("error", err))
	status := http.StatusInternalServerError
	if errors.Is(err, apperr.ErrNotFound) {
		status = http.StatusNotFound
	}
	http.Error(w, i18n.T(r.Context(), "daily.error"), status)
}
