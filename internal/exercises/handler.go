package exercises

import (
	"log/slog"
	"net/http"
	"strconv"

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

	// The catalog is 455 rows and a page is PageSize of them, so the list has
	// to be paged: before this it rendered the first 60 and there was no way
	// to reach the rest.
	requested := pageParam(r)
	filter.Limit = PageSize
	filter.Offset = (requested - 1) * PageSize

	found, total, err := h.svc.Search(ctx, filter)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// A page past the end renders empty, which looks like a filter that matched
	// nothing rather than a number that was too big. Re-run at the last real
	// page instead. Only ever one extra query, and only for an out-of-range
	// request.
	if last := lastPage(total); requested > last {
		requested = last
		filter.Offset = (requested - 1) * PageSize
		if found, total, err = h.svc.Search(ctx, filter); err != nil {
			h.fail(w, r, err)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := exercisepages.Browse(user, found, exercisepages.Filters{
		Query:     filter.Query,
		Muscle:    r.URL.Query().Get("muscle"),
		Category:  r.URL.Query().Get("category"),
		Equipment: r.URL.Query().Get("equipment"),
	}, exercisepages.Page{
		Number:   requested,
		Last:     lastPage(total),
		Total:    total,
		FirstRow: filter.Offset + 1,
	})
	if err := page.Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render exercise browse", slog.Any("error", err))
	}
}

// pageParam reads ?page=, 1-based.
//
// Anything unparseable or below 1 is page 1 rather than an error: the number
// arrives from a URL someone may have typed or truncated, and the first page is
// a better answer than a 400 on a page that is only a list.
func pageParam(r *http.Request) int {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// lastPage is never 0: an empty result still has a page 1 to render the "nothing
// matched" message on, and a 0 would make the clamp above ask for a negative
// offset.
func lastPage(total int) int {
	if total <= 0 {
		return 1
	}
	return (total + PageSize - 1) / PageSize
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
