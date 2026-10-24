package insights

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
	awardpages "github.com/NorthAIProject/north-client/web/awards"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// Badges reads the badges a person holds. awards.Service satisfies it.
type Badges interface {
	Earned(ctx context.Context, userID uuid.UUID) ([]awards.Earned, error)
	Monthly(ctx context.Context, userID uuid.UUID, now time.Time) (awards.MonthlyChallenge, error)
}

type Handler struct {
	svc    *Service
	badges Badges
}

// WithAwards puts the awards card on the summary.
func (h *Handler) WithAwards(b Badges) *Handler { h.badges = b; return h }

// awardsCard is the owner's medals for the summary page, or nil when badges
// are not wired or cannot be read: a missing card must not break the page.
func (h *Handler) awardsCard(r *http.Request) templ.Component {
	if h.badges == nil {
		return nil
	}
	ctx := r.Context()
	held, err := h.badges.Earned(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		middleware.FromContext(ctx).Warn("insights: awards card skipped", slog.Any("error", err))
		return nil
	}
	var recent []award.View
	for i := len(held) - 1; i >= 0 && len(recent) < 6; i-- {
		at := held[i].At
		recent = append(recent, held[i].Badge.Project(ctx, &at, nil, ""))
	}
	// The challenge line is a bonus: if it cannot be read, the card still shows.
	var monthly *award.MonthlyChallengeView
	if c, err := h.badges.Monthly(ctx, auth.MustUser(ctx).ID, time.Now()); err != nil {
		middleware.FromContext(ctx).Warn("insights: monthly challenge skipped", slog.Any("error", err))
	} else {
		v := awards.ProjectMonthlyChallenge(ctx, c)
		monthly = &v
	}
	return awardpages.SummaryCard(len(held), len(award.Catalog()), recent, monthly)
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
		r.Get("/", h.summary)
		r.Get("/panels", h.summaryPanels)

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

		r.Get("/nutrition", h.nutrition)
		r.Get("/nutrition/body", h.nutritionBody)

		r.Get("/sleep", h.statsPage("sleep", false))
		r.Get("/sleep/body", h.statsPage("sleep", true))
		r.Get("/cardio", h.statsPage("cardio", false))
		r.Get("/cardio/body", h.statsPage("cardio", true))
		r.Get("/patterns", h.statsPage("patterns", false))
		r.Get("/patterns/body", h.statsPage("patterns", true))

		r.Get("/coach", h.coach)
		r.Get("/coach/body", h.coachBody)

		r.Get("/spend", h.spend)
		r.Get("/spend/body", h.spendBody)

		// Parameterised, so the route test skips it rather than demanding a
		// registry row per metric. Nine detail pages in the sidebar would
		// bury the five sections that matter.
		r.Get("/metric/{key}", h.metric)
		r.Get("/metric/{key}/body", h.metricBody)
	})
}

// summary is the section's landing page: every domain's score at once, with
// each ring a link into the detail page it summarises.
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Summary(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildSummaryView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Summary(user, view, h.awardsCard(r)))
}

func (h *Handler) summaryPanels(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Summary(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildSummaryView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.SummaryPanels(view))
}

func (h *Handler) timeline(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Timeline(r.Context(), user, rg, r.URL.Query().Get("kind"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Timeline(user, buildTimelineView(r.Context(), data)))
}

func (h *Handler) timelineBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Timeline(r.Context(), user, rg, r.URL.Query().Get("kind"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.TimelineBody(buildTimelineView(r.Context(), data)))
}

func (h *Handler) body(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Body(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildBodyView(r.Context(), data)
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
	view, err := buildBodyView(r.Context(), data)
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
	view, err := buildMindView(r.Context(), data)
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
	view, err := buildMindView(r.Context(), data)
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
	view, err := buildProgressView(r.Context(), data)
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
	view, err := buildProgressView(r.Context(), data)
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
	view, err := buildTrainingView(r.Context(), data)
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
	view, err := buildTrainingView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.TrainingPanels(view))
}

// context resolves the reader and the window they asked for. Parse never
// fails, so a hand-typed ?range= cannot take a page down.
func (h *Handler) nutrition(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Nutrition(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildNutritionView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Nutrition(user, view))
}

func (h *Handler) nutritionBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Nutrition(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildNutritionView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.NutritionBody(view))
}

func (h *Handler) coach(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Coach(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildCoachView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Coach(user, view))
}

func (h *Handler) coachBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Coach(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildCoachView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.CoachBody(view))
}

func (h *Handler) spend(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Spend(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildSpendView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.Spend(user, view))
}

func (h *Handler) spendBody(w http.ResponseWriter, r *http.Request) {
	user, rg := h.context(r)
	data, err := h.svc.Spend(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := buildSpendView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, insightpages.SpendBody(view))
}

func (h *Handler) metric(w http.ResponseWriter, r *http.Request) {
	view, ok := h.metricView(w, r)
	if !ok {
		return
	}
	h.render(w, r, insightpages.Metric(auth.MustUser(r.Context()), view))
}

func (h *Handler) metricBody(w http.ResponseWriter, r *http.Request) {
	view, ok := h.metricView(w, r)
	if !ok {
		return
	}
	h.render(w, r, insightpages.MetricBody(view))
}

// metricView loads and builds the view both metric handlers render, reporting
// whether it already answered the request with an error.
func (h *Handler) metricView(w http.ResponseWriter, r *http.Request) (insightpages.MetricView, bool) {
	user, rg := h.context(r)

	data, err := h.svc.Metric(r.Context(), user, rg, chi.URLParam(r, "key"))
	if err != nil {
		h.fail(w, r, err)
		return insightpages.MetricView{}, false
	}
	view, err := buildMetricView(r.Context(), data)
	if err != nil {
		h.fail(w, r, err)
		return insightpages.MetricView{}, false
	}
	return view, true
}

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

// statsPage renders one of the stats pages, whole or as the panels a range
// switch swaps in.
func (h *Handler) statsPage(kind string, panels bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, rg := h.context(r)
		stats := h.svc.stats
		if stats == nil {
			h.fail(w, r, apperr.ErrNotFound)
			return
		}
		ctx := r.Context()
		switch kind {
		case "sleep":
			st, err := stats.Sleep(ctx, user, rg)
			if err != nil {
				h.fail(w, r, err)
				return
			}
			view := buildSleepView(ctx, rg, st)
			if panels {
				h.render(w, r, insightpages.SleepPanels(view))
				return
			}
			h.render(w, r, insightpages.Sleep(user, view))
		case "cardio":
			st, err := stats.Cardio(ctx, user, rg)
			if err != nil {
				h.fail(w, r, err)
				return
			}
			view := buildCardioView(ctx, rg, st)
			if panels {
				h.render(w, r, insightpages.CardioPanels(view))
				return
			}
			h.render(w, r, insightpages.Cardio(user, view))
		default:
			found, days, err := stats.Patterns(ctx, user, rg)
			if err != nil {
				h.fail(w, r, err)
				return
			}
			view := buildPatternsView(ctx, rg, days, found)
			if panels {
				h.render(w, r, insightpages.PatternsPanels(view))
				return
			}
			h.render(w, r, insightpages.Patterns(user, view))
		}
	}
}
