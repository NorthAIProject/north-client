package insights

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts the review pages. Must be behind RequireAuth.
//
// Every page has a body endpoint beside it. The range selector swaps that,
// while the page's own URL stays a working link for anyone without
// JavaScript — the same arrangement the overview uses.
func (h *Handler) Routes(r chi.Router) {
	r.Route("/insights", func(r chi.Router) {
		r.Get("/", h.redirectToTimeline)

		r.Get("/timeline", h.timeline)
		r.Get("/timeline/body", h.timelineBody)

		r.Get("/body", h.body)
		r.Get("/body/body", h.bodyBody)

		r.Get("/mind", h.mind)
		r.Get("/mind/body", h.mindBody)

		r.Get("/progress", h.progress)
		r.Get("/progress/body", h.progressBody)

		r.Get("/training", h.training)
		r.Get("/training/body", h.trainingBody)
	})
}

func (h *Handler) redirectToTimeline(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/app/insights/timeline", http.StatusSeeOther)
}

func (h *Handler) timeline(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Timeline(r.Context(), user, rg, r.URL.Query().Get("kind"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Timeline(user, buildTimelineView(data)))
}

func (h *Handler) timelineBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Timeline(r.Context(), user, rg, r.URL.Query().Get("kind"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.TimelineBody(buildTimelineView(data)))
}

func (h *Handler) body(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Body(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildBodyView(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Body(user, view))
}

func (h *Handler) bodyBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Body(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildBodyView(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.BodyPanels(view))
}

func (h *Handler) mind(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Mind(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildMindView(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Mind(user, view))
}

func (h *Handler) mindBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Mind(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildMindView(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.MindPanels(view))
}

func (h *Handler) progress(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Progress(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildProgressView(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Progress(user, view))
}

func (h *Handler) progressBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Progress(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildProgressView(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.ProgressPanels(view))
}

func (h *Handler) training(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Training(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildTrainingView(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Training(user, view))
}

func (h *Handler) trainingBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Training(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildTrainingView(data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.TrainingPanels(view))
}

// context resolves the reader and the window they asked for. Parse never
// fails, so a hand-typed ?range= cannot take a page down.
func (h *Handler) context(r *http.Request) (users.User, timerange.Range) {
	user := auth.MustUser(r.Context())
	return user, timerange.Parse(r.URL.Query().Get("range"), user.Location())
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render insights", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("insights request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
