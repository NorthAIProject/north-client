package reports

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	reportpages "github.com/NorthAIProject/north-client/web/reports"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the review pages. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/reports", h.index)
	r.Post("/reports/generate", h.generate)
	r.Get("/reports/{id}", h.show)
	r.Post("/reports/{id}/archive", h.archive)
	r.Post("/reports/{id}/regenerate", h.regenerate)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	archived := r.URL.Query().Get("archived") == "1"

	list, err := h.svc.List(r.Context(), user.ID, archived)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, reportpages.IndexPage(user, list, reportpages.IndexView{
		Archived: archived,
		Notice:   r.URL.Query().Get("notice"),
	}))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	report, err := h.svc.Get(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, reportpages.DetailPage(user, report, r.URL.Query().Get("notice")))
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	report, err := h.svc.RequestGenerate(r.Context(), user.ID, time.Time{})
	if err != nil {
		if apperr.Is(err, apperr.ErrConflict) {
			http.Redirect(w, r, "/app/reports?notice=cooldown", http.StatusSeeOther)
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/reports/"+report.ID.String(), http.StatusSeeOther)
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.svc.Archive(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/reports", http.StatusSeeOther)
}

func (h *Handler) regenerate(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	updated, err := h.svc.Regenerate(r.Context(), id, user.ID)
	switch {
	case err == nil:
		http.Redirect(w, r, "/app/reports/"+updated.ID.String(), http.StatusSeeOther)
	case errors.Is(err, ErrArchived):
		http.Redirect(w, r, "/app/reports/"+id.String()+"?notice=archived", http.StatusSeeOther)
	case apperr.Is(err, apperr.ErrConflict):
		http.Redirect(w, r, "/app/reports/"+id.String()+"?notice=cooldown", http.StatusSeeOther)
	default:
		h.fail(w, r, err)
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrConflict):
		http.Error(w, "Already generated this hour.", http.StatusConflict)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("report request failed", slog.Any("error", err))
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
