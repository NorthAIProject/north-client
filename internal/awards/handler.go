package awards

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	pages "github.com/NorthAIProject/north-client/web/awards"
)

// Person is whose awards page this is, as far as the viewer may know.
type Person struct {
	ID     uuid.UUID
	Name   string
	Handle string
	Self   bool
	// Shared is whether the person lets followers see their awards. The
	// owner's own is always true.
	Shared bool
}

// People resolves handles. It lives in profiles, which already knows about
// blocks and sharing; awards cannot import it back.
type People interface {
	// Person is ErrNotFound for an unknown handle or a block either way,
	// the same as the profile page.
	Person(ctx context.Context, viewer users.User, handle string) (Person, error)
	// HandleOf is the person's handle, empty when they have none.
	HandleOf(ctx context.Context, userID uuid.UUID) (string, error)
}

// Handler is the awards pages and the unlock moment under /app.
type Handler struct {
	svc    *Service
	people People
}

func NewHandler(svc *Service, people People) *Handler {
	return &Handler{svc: svc, people: people}
}

// Routes mount under /app, behind the session.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/awards", h.own)
	r.Get("/awards/unseen", h.unseen)
	r.Post("/awards/seen", h.seenBatch)
	r.Post("/awards/{badgeID}/seen", h.seenOne)
	r.Get("/u/{handle}/awards", h.profile)
}

func (h *Handler) views(ctx context.Context, earned []Earned) ([]award.View, error) {
	rarities, err := h.svc.Rarities(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]award.View, 0, len(earned))
	for _, e := range earned {
		out = append(out, projectEarned(ctx, e, rarities))
	}
	return out, nil
}

// unseen answers 204 when nothing is waiting: htmx 4 does not swap a 204, so
// the shell's host stays empty and nothing opens.
func (h *Handler) unseen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	unseen, err := h.svc.Unseen(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if len(unseen) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	views, err := h.views(ctx, unseen)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, pages.Unlock(views))
}

func (h *Handler) seenOne(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "badgeID")
	if _, ok := award.ByID(id); !ok {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.svc.MarkSeen(r.Context(), auth.MustUser(r.Context()).ID, id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// seenBatch marks every badge named in the form as seen. With to=profile it
// then goes to the caller's profile (their awards page without a handle),
// which is the "View on profile" button; otherwise it is a bare 204.
func (h *Handler) seenBatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	user := auth.MustUser(ctx)
	ids := r.PostForm["badge"]
	for _, id := range ids {
		if _, ok := award.ByID(id); !ok {
			h.fail(w, r, apperr.ErrValidation)
			return
		}
	}
	for _, id := range ids {
		if err := h.svc.MarkSeen(ctx, user.ID, id); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	if r.PostFormValue("to") != "profile" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	to := "/app/awards"
	if handle, err := h.people.HandleOf(ctx, user.ID); err == nil && handle != "" {
		to = "/app/u/" + url.PathEscape(handle)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// own is the caller's page, for people without a handle too.
func (h *Handler) own(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	handle, err := h.people.HandleOf(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.page(w, r, Person{ID: user.ID, Name: user.DisplayName, Handle: handle, Self: true, Shared: true})
}

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	p, err := h.people.Person(r.Context(), auth.MustUser(r.Context()), chi.URLParam(r, "handle"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.page(w, r, p)
}

// page is the owner's earned and locked badges, or another person's shared,
// public ones.
func (h *Handler) page(w http.ResponseWriter, r *http.Request, p Person) {
	ctx := r.Context()
	d := pages.PageData{User: auth.MustUser(ctx), Name: p.Name, Handle: p.Handle, Self: p.Self}
	if p.Self || p.Shared {
		earned, err := h.svc.Earned(ctx, p.ID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		held := map[string]bool{}
		shown := earned[:0:0]
		for _, e := range earned {
			held[e.Badge.ID] = true
			// A private badge never leaves its owner, whatever the switch.
			if p.Self || !e.Badge.Area.Private() {
				shown = append(shown, e)
			}
		}
		if d.Earned, err = h.views(ctx, shown); err != nil {
			h.fail(w, r, err)
			return
		}
		if p.Self {
			progress, err := h.svc.Progress(ctx, p.ID)
			if err != nil {
				h.fail(w, r, err)
				return
			}
			rarities, err := h.svc.Rarities(ctx)
			if err != nil {
				h.fail(w, r, err)
				return
			}
			for _, b := range award.Catalog() {
				if held[b.ID] {
					continue
				}
				n := min(progress[b.ID], b.Target)
				d.Locked = append(d.Locked, b.Project(ctx, nil, &n, rarities[b.ID].Label()))
			}
		}
	}
	h.render(w, r, pages.Page(d))
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
		middleware.FromContext(r.Context()).Error("awards request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
