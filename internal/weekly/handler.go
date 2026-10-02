package weekly

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
	weeklypages "github.com/NorthAIProject/north-client/web/weekly"
)

// Handler serves the weekly review page.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mount under /app.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/weekly", h.page)
	r.Post("/weekly", h.save)
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	rv, err := h.svc.Review(r.Context(), user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	f := weeklypages.FormFrom(rv.Current)
	f.Saved = r.URL.Query().Get("saved") == "1"
	h.render(w, r, http.StatusOK, weeklypages.Page(user, rv, f))
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	user := auth.MustUser(r.Context())
	in := Input{Priorities: r.PostForm["priority"], GoalOrder: goalOrder(r), Volume: plan.Volume(r.PostFormValue("volume"))}
	if _, err := h.svc.SetFocus(r.Context(), user, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if !apperr.As(err, &fieldErrs) {
			h.fail(w, r, err)
			return
		}
		rv, reviewErr := h.svc.Review(r.Context(), user)
		if reviewErr != nil {
			h.fail(w, r, reviewErr)
			return
		}
		f := weeklypages.Form{Volume: in.Volume, Errors: fieldErrs.Messages()}
		copy(f.Priorities[:], in.Priorities)
		h.render(w, r, http.StatusUnprocessableEntity, weeklypages.Page(user, rv, f))
		return
	}
	http.Redirect(w, r, "/app/weekly?saved=1", http.StatusSeeOther)
}

// goalOrder reads the rank_<goal id> fields into goal ids, lowest rank first.
// Blank or invalid ranks go last; ties fall back to a stable id order, since
// form fields arrive in no particular order.
func goalOrder(r *http.Request) []uuid.UUID {
	type ranked struct {
		id   uuid.UUID
		rank int
	}
	var list []ranked
	for key, values := range r.PostForm {
		raw, ok := strings.CutPrefix(key, "rank_")
		if !ok || len(values) == 0 {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		rank, err := strconv.Atoi(strings.TrimSpace(values[0]))
		if err != nil || rank < 1 {
			rank = 1 << 30
		}
		list = append(list, ranked{id: id, rank: rank})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].rank != list[j].rank {
			return list[i].rank < list[j].rank
		}
		return list[i].id.String() < list[j].id.String()
	})
	out := make([]uuid.UUID, 0, len(list))
	for _, g := range list {
		out = append(out, g.id)
	}
	return out
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	if apperr.Is(err, apperr.ErrValidation) {
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
		return
	}
	middleware.FromContext(r.Context()).Error("weekly review failed", slog.Any("error", err))
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
