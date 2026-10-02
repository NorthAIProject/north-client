package inbox

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/inbox/item"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	inboxpages "github.com/NorthAIProject/north-client/web/inbox"
)

// Handler serves the inbox page.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/inbox", h.index)
	r.Post("/inbox", h.add)
	r.Post("/inbox/{itemID}/file", h.file)
	r.Post("/inbox/{itemID}/dismiss", h.dismiss)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.page(w, r, http.StatusOK, inboxpages.AddForm{})
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request, status int, f inboxpages.AddForm) {
	user := auth.MustUser(r.Context())
	items, open, err := h.svc.Open(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	active, err := h.svc.goals.ListActive(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, status, inboxpages.Page(user, items, open, active, f))
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	text := r.PostFormValue("text")
	if _, err := h.svc.Add(r.Context(), auth.MustUser(r.Context()).ID, item.SourceWeb, text); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			h.page(w, r, http.StatusUnprocessableEntity, inboxpages.AddForm{Text: text, Error: fieldErrs.Messages()["text"]})
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/inbox", http.StatusSeeOther)
}

func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil || r.ParseForm() != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	f := Filing{Destination: r.PostFormValue("destination"), Title: strings.TrimSpace(r.PostFormValue("title"))}
	if g, parseErr := uuid.Parse(r.PostFormValue("goal_id")); parseErr == nil {
		f.GoalID = g
	}
	if _, err = h.svc.File(r.Context(), auth.MustUser(r.Context()).ID, id, f); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/inbox", http.StatusSeeOther)
}

func (h *Handler) dismiss(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err = h.svc.Dismiss(r.Context(), auth.MustUser(r.Context()).ID, id); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/inbox", http.StatusSeeOther)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			for _, msg := range fieldErrs.Messages() {
				http.Error(w, msg, http.StatusUnprocessableEntity)
				return
			}
		}
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("inbox request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
