package memories

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	memorypages "github.com/NorthAIProject/north-client/web/memories"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts memory endpoints. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/memories", h.index)
	r.Post("/memories", h.create)
	r.Post("/memories/{id}", h.update)
	r.Post("/memories/{id}/approve", h.approve)
	r.Post("/memories/{id}/reject", h.reject)
	r.Post("/memories/{id}/pin", h.pin)
	r.Post("/memories/{id}/unpin", h.unpin)
	r.Post("/memories/{id}/delete", h.destroy)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	pending, err := h.svc.ListPending(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	approved, err := h.svc.ListApproved(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, memorypages.IndexPage(user, pending, approved, memorypages.MemoryForm{}))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := memorypages.MemoryForm{
		Category: strings.TrimSpace(r.PostFormValue("category")),
		Content:  strings.TrimSpace(r.PostFormValue("content")),
	}

	if _, err := h.svc.Create(r.Context(), user.ID, Input{Category: form.Category, Content: form.Content}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			pending, _ := h.svc.ListPending(r.Context(), user.ID)
			approved, _ := h.svc.ListApproved(r.Context(), user.ID)
			render(w, r, http.StatusUnprocessableEntity, memorypages.IndexPage(user, pending, approved, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/memories", http.StatusSeeOther)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	if _, err := h.svc.Update(r.Context(), id, user.ID, Input{
		Category: strings.TrimSpace(r.PostFormValue("category")),
		Content:  strings.TrimSpace(r.PostFormValue("content")),
	}); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/memories", http.StatusSeeOther)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	h.statusAction(w, r, h.svc.Approve)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	h.statusAction(w, r, h.svc.Reject)
}

func (h *Handler) pin(w http.ResponseWriter, r *http.Request) {
	h.pinAction(w, r, true)
}

func (h *Handler) unpin(w http.ResponseWriter, r *http.Request) {
	h.pinAction(w, r, false)
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.svc.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/memories", http.StatusSeeOther)
}

func (h *Handler) statusAction(w http.ResponseWriter, r *http.Request, fn func(context.Context, uuid.UUID, uuid.UUID) (Memory, error)) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if _, err := fn(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/memories", http.StatusSeeOther)
}

func (h *Handler) pinAction(w http.ResponseWriter, r *http.Request, pinned bool) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if _, err := h.svc.SetPinned(r.Context(), id, user.ID, pinned); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/memories", http.StatusSeeOther)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("memory request failed", slog.Any("error", err))
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
