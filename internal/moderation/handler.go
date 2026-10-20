package moderation

import (
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	moderationpages "github.com/NorthAIProject/north-client/web/moderation"
)

// Handler serves accepting the terms and the operator queue, under /app.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Post("/moderation/terms", h.acceptTerms)
	r.Get("/ops/moderation", h.queue)
	r.Post("/ops/moderation/users/{userID}/suspend", h.suspend)
	r.Post("/ops/moderation/{kind}/{subjectID}/{action}", h.decide)
}

// acceptTerms records the current terms and goes back to next, a local
// path, or /app.
func (h *Handler) acceptTerms(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.AcceptTerms(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		http.Error(w, "The rules could not be saved.", http.StatusInternalServerError)
		return
	}
	next := r.FormValue("next")
	if !auth.SafeRedirect(next) {
		next = "/app"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if !h.svc.IsOperator(user.ID) {
		http.NotFound(w, r)
		return
	}
	groups, err := h.svc.OpenReports(r.Context())
	if err != nil {
		http.Error(w, "The reports could not be loaded.", http.StatusInternalServerError)
		return
	}
	view := make([]moderationpages.Group, 0, len(groups))
	for _, g := range groups {
		view = append(view, moderationpages.Group{
			Kind: g.Kind, SubjectID: g.SubjectID, AuthorID: g.AuthorID, Reasons: g.Reasons,
			Hidden: g.Hidden, Excerpt: g.Excerpt, FirstAt: g.FirstAt,
		})
	}
	render(w, r, moderationpages.OpsPage(user, view))
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "subjectID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.svc.Decide(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "kind"), id, chi.URLParam(r, "action"))
	if !h.done(w, r, err) {
		return
	}
	http.Redirect(w, r, "/app/ops/moderation", http.StatusSeeOther)
}

// suspend stops a user posting for hours, or until lifted when hours is 0.
func (h *Handler) suspend(w http.ResponseWriter, r *http.Request) {
	op := auth.MustUser(r.Context()).ID
	if !h.svc.IsOperator(op) {
		http.NotFound(w, r)
		return
	}
	target, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var until *time.Time
	if hours, convErr := strconv.Atoi(r.FormValue("hours")); convErr == nil && hours > 0 {
		t := time.Now().Add(time.Duration(hours) * time.Hour)
		until = &t
	}
	if !h.done(w, r, h.svc.Suspend(r.Context(), &op, target, until, "operator")) {
		return
	}
	http.Redirect(w, r, "/app/ops/moderation", http.StatusSeeOther)
}

// done writes the failure for err and reports whether there was none.
func (h *Handler) done(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case apperr.Is(err, apperr.ErrNotFound):
		http.NotFound(w, r)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		http.Error(w, "That could not be saved.", http.StatusInternalServerError)
	}
	return false
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = c.Render(r.Context(), w)
}
