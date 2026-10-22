package profiles

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/xp"
	pages "github.com/NorthAIProject/north-client/web/profiles"
)

// Follows is the follow button's side of social.Service.
type Follows interface {
	Profile(ctx context.Context, viewerID uuid.UUID, handle string) (social.Profile, error)
	Follow(ctx context.Context, followerID uuid.UUID, handle string) (social.Connection, error)
	Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) error
}

// Handler is the profile pages at /app/u/{handle}.
type Handler struct {
	svc     *Service
	follows Follows
	now     func() time.Time
}

func NewHandler(svc *Service, follows Follows) *Handler {
	return &Handler{svc: svc, follows: follows, now: time.Now}
}

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/u/{handle}", h.profile)
	r.Get("/u/{handle}/workouts", h.workouts)
	r.Get("/u/{handle}/workouts/{sessionID}", h.workout)
	r.Post("/u/{handle}/follow", h.follow)
	r.Post("/u/{handle}/unfollow", h.unfollow)
}

func (h *Handler) data(r *http.Request, p social.Profile, shared bool) pages.Data {
	return pages.Data{
		User: auth.MustUser(r.Context()), Name: p.DisplayName, Handle: p.Handle,
		Self: p.Relationship.Self, Follows: p.Relationship.Following != "",
		Followers: p.Followers, Following: p.Following, Shared: shared,
	}
}

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.View(r.Context(), auth.MustUser(r.Context()), chi.URLParam(r, "handle"), h.now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	d := h.data(r, v.Profile, v.Shares.Training)
	d.Workouts, d.Recent = v.Workouts, v.Recent
	if v.Level != nil {
		l := xp.ProjectLevel(r.Context(), *v.Level)
		d.HasLevel, d.LevelNumber, d.LevelTitle = true, l.Number, l.Title
	}
	h.render(w, r, pages.ProfilePage(d))
}

func (h *Handler) workouts(w http.ResponseWriter, r *http.Request) {
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			h.fail(w, r, apperr.ErrValidation)
			return
		}
		before = t
	}
	user, handle := auth.MustUser(r.Context()), chi.URLParam(r, "handle")
	page, err := h.svc.Workouts(r.Context(), user, handle, before)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, err := h.follows.Profile(r.Context(), user.ID, handle)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	more := ""
	if page.Before != nil {
		more = "?before=" + url.QueryEscape(page.Before.UTC().Format(time.RFC3339Nano))
	}
	h.render(w, r, pages.WorkoutsPage(h.data(r, p, page.Shared), page.Items, more))
}

func (h *Handler) workout(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	user, handle := auth.MustUser(r.Context()), chi.URLParam(r, "handle")
	c, err := h.svc.Workout(r.Context(), user, handle, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, err := h.follows.Profile(r.Context(), user.ID, handle)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, pages.WorkoutPage(h.data(r, p, true), c))
}

func (h *Handler) follow(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
	if _, err := h.follows.Follow(r.Context(), auth.MustUser(r.Context()).ID, handle); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/u/"+url.PathEscape(handle), http.StatusSeeOther)
}

func (h *Handler) unfollow(w http.ResponseWriter, r *http.Request) {
	user, handle := auth.MustUser(r.Context()), chi.URLParam(r, "handle")
	p, err := h.follows.Profile(r.Context(), user.ID, handle)
	if err == nil {
		err = h.follows.Unfollow(r.Context(), user.ID, p.ID)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/u/"+url.PathEscape(handle), http.StatusSeeOther)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
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
		middleware.FromContext(r.Context()).Error("profile request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
