package decisions

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	decisionpages "github.com/NorthAIProject/north-client/web/decisions"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/decisions", h.index)
	r.Post("/decisions", h.create)
	r.Get("/decisions/{id}", h.show)
	r.Post("/decisions/{id}", h.update)
	r.Post("/decisions/{id}/delete", h.destroy)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	list, err := h.svc.List(r.Context(), user.ID, 50)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, decisionpages.IndexPage(user, list, decisionpages.DecisionForm{}))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)
	if _, err := h.svc.Create(r.Context(), user.ID, inputFrom(form)); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			list, listErr := h.svc.List(r.Context(), user.ID, 50)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			h.render(w, r, http.StatusUnprocessableEntity, decisionpages.IndexPage(user, list, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/decisions", http.StatusSeeOther)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	d, err := h.svc.Get(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, decisionpages.ShowPage(user, d, decisionpages.FormFor(d)))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	existing, err := h.svc.Get(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	form := formFrom(r)
	in := inputFrom(form)
	// v1 forms do not collect outcome. Keep whatever is already stored so
	// an edit cannot wipe a later review.
	in.Outcome = existing.Outcome

	updated, err := h.svc.Update(r.Context(), id, user.ID, in)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.render(w, r, http.StatusUnprocessableEntity, decisionpages.ShowPage(user, existing, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/decisions/"+updated.ID.String(), http.StatusSeeOther)
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

	http.Redirect(w, r, "/app/decisions", http.StatusSeeOther)
}

func formFrom(r *http.Request) decisionpages.DecisionForm {
	return decisionpages.DecisionForm{
		Title:     strings.TrimSpace(r.PostFormValue("title")),
		Options:   strings.TrimSpace(r.PostFormValue("options")),
		Rationale: strings.TrimSpace(r.PostFormValue("rationale")),
	}
}

func inputFrom(form decisionpages.DecisionForm) Input {
	return Input{
		Title:     form.Title,
		Options:   form.Options,
		Rationale: form.Rationale,
	}
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
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("decision request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
