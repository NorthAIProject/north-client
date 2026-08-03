package goals

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	goalpages "github.com/NorthAIProject/north-client/web/goals"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the goal endpoints. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/goals", h.index)
	r.Post("/goals", h.create)
	r.Get("/goals/{id}", h.show)
	r.Post("/goals/{id}", h.update)
	r.Post("/goals/{id}/status", h.setStatus)
	r.Post("/goals/{id}/updates", h.addUpdate)
	r.Post("/goals/{id}/delete", h.destroy)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	list, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, goalpages.IndexPage(user, list, goalpages.GoalForm{}))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)

	goal, err := h.svc.Create(r.Context(), user.ID, inputFrom(form))
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			form.Open = true

			list, listErr := h.svc.List(r.Context(), user.ID)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			render(w, r, http.StatusUnprocessableEntity, goalpages.IndexPage(user, list, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+goal.ID.String(), http.StatusSeeOther)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	goal, err := h.svc.Get(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	updates, err := h.svc.Updates(r.Context(), goal.ID, 50)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, goalpages.DetailPage(user, goal, updates, goalpages.FormFor(goal)))
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

	form := formFrom(r)

	if _, err := h.svc.Update(r.Context(), id, user.ID, inputFrom(form)); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			goal, getErr := h.svc.Get(r.Context(), id, user.ID)
			if getErr != nil {
				h.fail(w, r, getErr)
				return
			}
			updates, _ := h.svc.Updates(r.Context(), id, 50)

			form.Errors = fieldErrs.Messages()
			form.Open = true
			render(w, r, http.StatusUnprocessableEntity, goalpages.DetailPage(user, goal, updates, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
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

	if _, err := h.svc.SetStatus(r.Context(), id, user.ID, r.PostFormValue("status")); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) addUpdate(w http.ResponseWriter, r *http.Request) {
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

	var progress *int
	if raw := strings.TrimSpace(r.PostFormValue("progress")); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil {
			progress = &n
		}
	}

	if _, err := h.svc.AddUpdate(r.Context(), id, user.ID, r.PostFormValue("note"), progress); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			goal, getErr := h.svc.Get(r.Context(), id, user.ID)
			if getErr != nil {
				h.fail(w, r, getErr)
				return
			}
			updates, _ := h.svc.Updates(r.Context(), id, 50)

			form := goalpages.FormFor(goal)
			form.UpdateError = fieldErrs.Messages()["note"]
			render(w, r, http.StatusUnprocessableEntity, goalpages.DetailPage(user, goal, updates, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+id.String(), http.StatusSeeOther)
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

	http.Redirect(w, r, "/app/goals", http.StatusSeeOther)
}

func formFrom(r *http.Request) goalpages.GoalForm {
	return goalpages.GoalForm{
		Title:      strings.TrimSpace(r.PostFormValue("title")),
		Motivation: strings.TrimSpace(r.PostFormValue("motivation")),
		Success:    strings.TrimSpace(r.PostFormValue("success")),
		Category:   strings.TrimSpace(r.PostFormValue("category")),
		TargetDate: strings.TrimSpace(r.PostFormValue("target_date")),
	}
}

func inputFrom(f goalpages.GoalForm) Input {
	in := Input{
		Title:      f.Title,
		Motivation: f.Motivation,
		Success:    f.Success,
		Category:   f.Category,
	}

	// An unparseable date is treated as no date rather than as an error: the
	// input is type="date", so anything else means the browser sent nothing.
	if f.TargetDate != "" {
		if parsed, err := time.Parse("2006-01-02", f.TargetDate); err == nil {
			in.TargetDate = parsed
		}
	}

	return in
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("goal request failed", slog.Any("error", err))
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
