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
	"github.com/NorthAIProject/north-client/internal/shared/htmx"
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
	r.Get("/memories/for/{conversationID}", h.forConversation)
	r.Post("/memories", h.create)
	r.Post("/memories/{id}", h.update)
	r.Post("/memories/{id}/approve", h.approve)
	r.Post("/memories/{id}/reject", h.reject)
	r.Post("/memories/{id}/pin", h.pin)
	r.Post("/memories/{id}/unpin", h.unpin)
	r.Post("/memories/{id}/exclude", h.exclude)
	r.Post("/memories/{id}/include", h.include)
	r.Post("/memories/{id}/delete", h.destroy)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, http.StatusOK, memorypages.MemoryForm{}, nil)
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, status int, form memorypages.MemoryForm, edit *memorypages.MemoryForm) {
	user := auth.MustUser(r.Context())
	pending, approved, err := h.lists(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	inst, err := buildInstruments(pending, approved)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	var component templ.Component
	if htmx.IsRequest(r) {
		component = memorypages.Panel(user, pending, approved, inst, form, edit)
	} else {
		component = memorypages.Page(user, pending, approved, inst, form, edit)
	}
	if err := component.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
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
			h.respond(w, r, http.StatusUnprocessableEntity, form, nil)
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

	form := memorypages.MemoryForm{
		ID:       id.String(),
		Category: strings.TrimSpace(r.PostFormValue("category")),
		Content:  strings.TrimSpace(r.PostFormValue("content")),
	}

	if _, err := h.svc.Update(r.Context(), id, user.ID, Input{Category: form.Category, Content: form.Content}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.respond(w, r, http.StatusUnprocessableEntity, memorypages.MemoryForm{}, &form)
			return
		}
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

func (h *Handler) exclude(w http.ResponseWriter, r *http.Request) {
	h.excludeAction(w, r, true)
}

func (h *Handler) include(w http.ResponseWriter, r *http.Request) {
	h.excludeAction(w, r, false)
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

func (h *Handler) forConversation(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	conversationID, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	h.renderConversationPending(w, r, user.ID, conversationID)
}

func (h *Handler) renderConversationPending(w http.ResponseWriter, r *http.Request, userID, conversationID uuid.UUID) {
	pending, err := h.svc.ListPendingForConversation(r.Context(), userID, conversationID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := memorypages.ConversationPending(conversationID, pending).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
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
	if conversationID, ok := parseConversationID(r); ok && htmx.IsRequest(r) {
		h.renderConversationPending(w, r, user.ID, conversationID)
		return
	}
	if htmx.IsRequest(r) {
		h.respond(w, r, http.StatusOK, memorypages.MemoryForm{}, nil)
		return
	}
	http.Redirect(w, r, "/app/memories", http.StatusSeeOther)
}

func parseConversationID(r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.FormValue("conversation_id"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	}
	if raw == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
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

func (h *Handler) excludeAction(w http.ResponseWriter, r *http.Request, excluded bool) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if _, err := h.svc.SetExcluded(r.Context(), id, user.ID, excluded); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/memories", http.StatusSeeOther)
}

func (h *Handler) lists(ctx context.Context, userID uuid.UUID) (pending, approved []Memory, err error) {
	pending, err = h.svc.ListPending(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	approved, err = h.svc.ListApproved(ctx, userID)
	return pending, approved, err
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
