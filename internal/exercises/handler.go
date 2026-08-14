package exercises

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	exercisepages "github.com/NorthAIProject/north-client/web/exercises"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/exercises", h.browse)
	r.Get("/exercises/{slug}", h.detail)
	r.Get("/exercises/{slug}/muscles", h.muscles)
}

func (h *Handler) browse(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	// The filter comes straight off the query string so a filtered view is a
	// URL someone can bookmark or share. Service.Search drops values outside
	// the vocabularies, so a hand-edited query cannot produce a silently empty
	// page.
	filter := Filter{
		Query:    r.URL.Query().Get("q"),
		Muscle:   r.URL.Query().Get("muscle"),
		Category: r.URL.Query().Get("category"),
	}
	if equipment := r.URL.Query().Get("equipment"); equipment != "" {
		filter.Equipment = []string{equipment}
	}

	found, total, err := h.svc.Search(ctx, filter)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := exercisepages.Browse(user, found, total, exercisepages.Filters{
		Query:     filter.Query,
		Muscle:    r.URL.Query().Get("muscle"),
		Category:  r.URL.Query().Get("category"),
		Equipment: r.URL.Query().Get("equipment"),
	})
	if err := page.Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render exercise browse", slog.Any("error", err))
	}
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	found, err := h.svc.GetBySlug(ctx, chi.URLParam(r, "slug"))
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := exercisepages.Detail(user, found).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render exercise detail", slog.Any("error", err))
	}
}

// muscles renders the viewer alone, for a page that knows a slug and nothing
// else about the exercise.
//
// The coach's chat is the caller: a reply that looked an exercise up records
// the slug, and the transcript fetches the picture rather than resolving every
// exercise mentioned anywhere in a conversation on every page load. Same
// reasoning as the sources disclosure in web/chat.
//
// An unknown slug renders nothing at all, with a 200. It arrives from a stored
// message that may be months old and may name an exercise since removed from
// the catalogue; a 404 in the middle of a transcript would be a broken-looking
// page for something that is merely out of date.
func (h *Handler) muscles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	found, err := h.svc.GetBySlug(ctx, chi.URLParam(r, "slug"))
	if err != nil {
		if !apperr.Is(err, apperr.ErrNotFound) {
			middleware.FromContext(ctx).Error("render exercise muscles", slog.Any("error", err))
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := exercisepages.MusclePartial(found).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render exercise muscles", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	middleware.FromContext(r.Context()).Error("exercise request failed", slog.Any("error", err))
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
