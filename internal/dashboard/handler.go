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
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	// Anchored to the user's own timezone, so "today" means their today.
	// An absent or unrecognised value resolves to timerange.DefaultKey rather
	// than erroring: a bookmarked or hand-edited URL should show the dashboard,
	// not a validation message.
	rg := timerange.Parse(r.URL.Query().Get("range"), user.Location())

	snap, err := h.svc.Load(r.Context(), user, rg)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	data, err := buildDashboardData(snap)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.Dashboard(user, data).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render dashboard", slog.Any("error", err))
	}
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

	return app.DashboardData{
		CheckedInToday:     snap.CheckedInToday,
		Streak:             snap.Streak,
		PendingMemories:    snap.PendingMemories,
		Goals:              snap.Goals,
		LastThread:         snap.LastThread,
		NextSession:        snap.NextSession,
		PlanID:             snap.PlanID,
		CheckIns:           mapCheckIns(snap.CheckIns),
		Habits:             mapHabits(snap.Habits),
		Hydration:          mapHydration(snap.Hydration),
		Sleep:              mapSleep(snap.Sleep),
		ActivityCalories7d: snap.ActivityCalories,
		MoodChart:          viz.MoodEnergyLine("dashboard-mood-energy", labels, mood, energy),
		HydrationChart:     viz.Bar("dashboard-hydration", "Water (ml)", hydrationLabels, hydrationTotals),
		HabitGaugeOption:   habitGaugeOption,
		CheckInHeatmap:     checkInHeatmapOption,
	}, nil
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
