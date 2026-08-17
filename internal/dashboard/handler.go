package dashboard

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/web/app"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts the overview. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.show)
	r.Get("/panels", h.panels)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	data, err := h.load(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.Dashboard(auth.MustUser(r.Context()), data).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render dashboard", slog.Any("error", err))
	}
}

// panels re-renders only the swappable body when the range selector changes.
//
// The selector's links carry a plain href to "/?range=" alongside their
// hx-get, so someone without JavaScript gets a whole page rather than nothing.
func (h *Handler) panels(w http.ResponseWriter, r *http.Request) {
	data, err := h.load(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.DashboardPanels(data).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render dashboard panels", slog.Any("error", err))
	}
}

func (h *Handler) load(r *http.Request) (app.DashboardData, error) {
	user := auth.MustUser(r.Context())

	// Parse never fails: a hand-typed ?range= falls back to today rather than
	// taking the page down.
	rg := timerange.Parse(r.URL.Query().Get("range"), user.Location())

	snap, err := h.svc.Load(r.Context(), user, rg)
	if err != nil {
		return app.DashboardData{}, err
	}
	return buildDashboardData(snap)
}

func buildDashboardData(snap Snapshot) (app.DashboardData, error) {
	labels := make([]string, len(snap.CheckIns.Days))
	mood := make([]int, len(snap.CheckIns.Days))
	energy := make([]int, len(snap.CheckIns.Days))
	heatmap := make([]viz.HeatmapCell, len(snap.CheckIns.Days))
	for i, d := range snap.CheckIns.Days {
		labels[i] = d.Label
		mood[i] = d.Mood
		energy[i] = d.Energy
		heatmap[i] = viz.HeatmapCell{Label: d.Label, Value: d.Mood}
	}

	hydrationLabels := make([]string, len(snap.Hydration.Days))
	hydrationTotals := make([]float64, len(snap.Hydration.Days))
	for i, d := range snap.Hydration.Days {
		hydrationLabels[i] = d.Label
		hydrationTotals[i] = float64(d.TotalML)
	}

	habitRaw, err := viz.GaugeOptionJSON("Adherence", snap.Habits.Rate)
	if err != nil {
		return app.DashboardData{}, err
	}
	habitGaugeOption, err := viz.UnmarshalOption(habitRaw)
	if err != nil {
		return app.DashboardData{}, err
	}
	heatmapRaw, err := viz.HeatmapJSON("Mood", heatmap)
	if err != nil {
		return app.DashboardData{}, err
	}
	checkInHeatmapOption, err := viz.UnmarshalOption(heatmapRaw)
	if err != nil {
		return app.DashboardData{}, err
	}

	activityDonut, hasDonut, err := buildKindDonut(snap.Timeline)
	if err != nil {
		return app.DashboardData{}, err
	}

	step, hasStep := PickNextStep(snap)

	return app.DashboardData{
		Range:            mapRange(snap.Range),
		CheckedInToday:   snap.CheckedInToday,
		Streak:           snap.Streak,
		GoalActivity7d:   snap.GoalActivity7d,
		PendingMemories:  snap.PendingMemories,
		Goals:            snap.Goals,
		LastThread:       snap.LastThread,
		NextSession:      snap.NextSession,
		PlanID:           snap.PlanID,
		CheckIns:         mapCheckIns(snap.CheckIns),
		Habits:           mapHabits(snap.Habits),
		Hydration:        mapHydration(snap.Hydration),
		Sleep:            mapSleep(snap.Sleep),
		ActivityCalories: snap.ActivityCalories,
		Timeline:         mapTimeline(snap.Timeline),
		Deltas:           mapDeltas(snap.Deltas),
		MoodChart:        viz.MoodEnergyLine("dashboard-mood-energy", labels, mood, energy),
		HydrationChart:   viz.Bar("dashboard-hydration", "Water (ml)", hydrationLabels, hydrationTotals),
		HabitGaugeOption: habitGaugeOption,
		CheckInHeatmap:   checkInHeatmapOption,
		ActivityDonut:    activityDonut,
		HasActivityDonut: hasDonut,
		Nudges:           snap.Nudges,
		Briefing:         briefingBody(snap),
		HasBriefing:      snap.Briefing != nil && snap.Briefing.Ready(),
		HasNextStep:      hasStep,
		NextStep: app.NextStep{
			Eyebrow: step.Eyebrow,
			Title:   step.Title,
			Body:    step.Body,
			CTA:     step.CTA,
			Href:    step.Href,
		},
	}, nil
}

// buildKindDonut splits the window's activity by which slice it came from.
// It is the "where did my effort go" panel, and it is built from the feed that
// is already in memory rather than from a twelfth query.
func buildKindDonut(feed []Entry) (map[string]any, bool, error) {
	if len(feed) == 0 {
		return nil, false, nil
	}

	counts := make(map[EntryKind]int, len(feed))
	order := make([]EntryKind, 0, len(feed))
	for _, e := range feed {
		if counts[e.Kind] == 0 {
			order = append(order, e.Kind)
		}
		counts[e.Kind]++
	}

	segments := make([]viz.DonutSegment, 0, len(order))
	for _, k := range order {
		segments = append(segments, viz.DonutSegment{Label: k.Label(), Value: counts[k]})
	}

	raw, err := viz.DonutOptionJSON(segments)
	if err != nil {
		return nil, false, err
	}
	option, err := viz.UnmarshalOption(raw)
	if err != nil {
		return nil, false, err
	}
	return option, true, nil
}

func mapRange(rg timerange.Range) app.RangeView {
	all := timerange.All(rg.Location())
	options := make([]app.RangeOption, len(all))
	for i, r := range all {
		options[i] = app.RangeOption{
			Key:      r.Key,
			Label:    r.Label,
			Selected: r.Key == rg.Key,
		}
	}
	return app.RangeView{Key: rg.Key, Label: rg.Label, Options: options}
}

func mapTimeline(feed []Entry) []app.TimelineEntryView {
	out := make([]app.TimelineEntryView, len(feed))
	for i, e := range feed {
		out[i] = app.TimelineEntryView{
			Kind:   string(e.Kind),
			Label:  e.Kind.Label(),
			At:     e.At,
			Title:  e.Title,
			Detail: e.Detail,
			Href:   e.Href,
			Icon:   e.Icon,
		}
	}
	return out
}

func mapDeltas(d Deltas) app.DeltasView {
	return app.DeltasView{
		Hydration:  mapDelta(d.Hydration),
		SleepHours: mapDelta(d.SleepHours),
		Calories:   mapDelta(d.Calories),
		CheckIns:   mapDelta(d.CheckIns),
	}
}

func mapDelta(d Delta) app.DeltaView {
	return app.DeltaView{
		Pct:       d.Pct,
		Direction: d.Direction,
		HasPrior:  d.HasPrior,
	}
}

func mapCheckIns(s CheckInSeries) app.CheckInSeriesView {
	out := app.CheckInSeriesView{Days: make([]app.CheckInDayView, len(s.Days))}
	for i, d := range s.Days {
		out.Days[i] = app.CheckInDayView{Label: d.Label, Mood: d.Mood, Energy: d.Energy}
	}
	return out
}

func mapHabits(h HabitsSummary) app.HabitsView {
	return app.HabitsView{
		HasHabits:  h.HasHabits,
		Rate:       h.Rate,
		Kept:       h.Kept,
		Scheduled:  h.Scheduled,
		BestStreak: h.BestStreak,
	}
}

func mapHydration(h HydrationSummary) app.HydrationView {
	out := app.HydrationView{
		TodayML:  h.TodayML,
		TargetML: h.TargetML,
		Percent:  h.Percent,
		Days:     make([]app.HydrationDayView, len(h.Days)),
	}
	for i, d := range h.Days {
		out.Days[i] = app.HydrationDayView{Label: d.Label, TotalML: d.TotalML, TargetML: d.TargetML}
	}
	return out
}

func mapSleep(s SleepSummary) app.SleepView {
	return app.SleepView{
		Logged:          s.Logged,
		DurationMinutes: s.DurationMinutes,
		Quality:         s.Quality,
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("dashboard request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

// briefingBody is the markdown the card renders. A briefing that is still
// pending or that failed has no body worth showing, so the card stays hidden
// rather than rendering an empty box.
func briefingBody(snap Snapshot) string {
	if snap.Briefing == nil || !snap.Briefing.Ready() {
		return ""
	}
	return snap.Briefing.Body
}
