package day

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	daypages "github.com/NorthAIProject/north-client/web/day"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts My Day at /app. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.show)
	r.Post("/day/rules", h.saveRules)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	date := ParseDate(r.URL.Query().Get("date"), user.Location(), time.Now())

	snap, err := h.svc.Load(r.Context(), user, date)
	if err != nil {
		middleware.FromContext(r.Context()).Error("load day", slog.Any("error", err))
		http.Error(w, i18n.T(r.Context(), "day.error"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := daypages.Page(user, BuildView(r.Context(), snap)).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render day", slog.Any("error", err))
	}
}

// saveRules takes the whole rules form at once: one row per kind, a checkbox
// and a time. A kind left blank is removed rather than stored empty.
func (h *Handler) saveRules(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(r.Context(), "day.error"), http.StatusBadRequest)
		return
	}

	for _, kind := range day.RuleKinds() {
		at := r.PostFormValue("at_" + string(kind))
		var err error
		if at == "" {
			err = h.svc.DeleteRule(r.Context(), user.ID, kind)
		} else {
			_, err = h.svc.SetRule(r.Context(), user.ID, day.Rule{
				Kind: kind, At: at, Enabled: r.PostFormValue("on_"+string(kind)) == "true",
			})
		}
		if err != nil {
			middleware.FromContext(r.Context()).Warn("save day rule", slog.String("kind", string(kind)), slog.Any("error", err))
			http.Error(w, i18n.T(r.Context(), "day.rules.invalid"), http.StatusUnprocessableEntity)
			return
		}
	}

	target := "/app"
	if d := r.PostFormValue("date"); d != "" {
		target += "?date=" + url.QueryEscape(d)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// BuildView turns a snapshot into what the page draws.
func BuildView(ctx context.Context, s Snapshot) daypages.Data {
	date := s.Date.Format("2006-01-02")
	hrefFor := func(t time.Time) string { return "/app?date=" + t.Format("2006-01-02") }

	data := daypages.Data{
		Date:      date,
		DateLabel: dateLabel(ctx, s.Date),
		IsToday:   s.IsToday,
		PrevHref:  hrefFor(s.Date.AddDate(0, 0, -1)),
		NextHref:  hrefFor(s.Date.AddDate(0, 0, 1)),
		TodayHref: "/app",
		NowLabel:  s.Now.Format("15:04"),
		Streak:    s.Streak,
	}

	data.Vitals = []daypages.Vital{
		percentVital("energy", s.EnergyPercent, "var(--color-day-move)"),
		minutesVital("sunlight", s.DaylightMinutes, 60, "var(--color-day-sun)"),
		{Key: "water", Value: litres(s.Water.TotalML), Unit: "L", Fraction: s.Water.Fraction(), Color: "var(--color-day-water)"},
		{Key: "move", Value: fmt.Sprintf("%.0f", s.Activity.Move.Value), Unit: "kcal", Fraction: s.Activity.Move.Fraction(), Color: "var(--color-day-move)"},
		streakVital(s.Streak),
	}

	data.Food = foodCard(s.Food)
	data.Water = daypages.WaterCard{
		TotalML:  fmt.Sprintf("%d", s.Water.TotalML),
		TargetML: fmt.Sprintf("%d", s.Water.TargetML),
		Litres:   litres(s.Water.TotalML) + " L",
		Fraction: s.Water.Fraction(),
	}
	data.Activity = daypages.ActivityCard{Rings: []daypages.Ring{
		ring("move", s.Activity.Move, "kcal", "var(--color-day-move)", 33),
		ring("exercise", s.Activity.Exercise, "min", "var(--color-day-exercise)", 23),
		ring("stand", s.Activity.Stand, "h", "var(--color-day-stand)", 13),
	}}
	data.Sleep = sleepCard(s.Sleep)
	data.Workouts = daypages.WorkoutsCard{
		Count:   s.Workouts.Count,
		Minutes: fmt.Sprintf("%d", s.Workouts.Minutes),
		Labels:  s.Workouts.Labels,
	}
	data.Body = bodyCard(s.Body)

	var items []daypages.RailInput
	for _, e := range s.Timeline {
		items = append(items, daypages.RailInput{At: e.At, Title: e.Title, Detail: e.Detail, Href: e.Href, Color: kindColor(e.Kind)})
	}
	var markers []daypages.MarkerInput
	for _, m := range s.Markers {
		markers = append(markers, daypages.MarkerInput{At: m.At, Kind: string(m.Kind), Passed: m.Passed})
	}
	data.Rail = daypages.BuildRail(s.Date, items, markers, s.Now, s.IsToday)

	set := map[day.RuleKind]day.Rule{}
	for _, r := range s.Rules {
		set[r.Kind] = r
	}
	for _, k := range day.RuleKinds() {
		r, ok := set[k]
		data.Rules = append(data.Rules, daypages.RuleRow{Kind: string(k), At: r.At, Enabled: ok && r.Enabled})
	}
	return data
}

func dateLabel(ctx context.Context, t time.Time) string {
	return fmt.Sprintf("%s, %d %s",
		i18n.T(ctx, fmt.Sprintf("weekday.long.%d", int(t.Weekday()))),
		t.Day(),
		i18n.T(ctx, fmt.Sprintf("month.long.%d", int(t.Month()))))
}

func percentVital(key string, v *int, color string) daypages.Vital {
	out := daypages.Vital{Key: key, Unit: "%", Color: color}
	if v != nil {
		out.Value = fmt.Sprintf("%d", *v)
		out.Fraction = float64(*v) / 100
	}
	return out
}

func minutesVital(key string, v *int, goal int, color string) daypages.Vital {
	out := daypages.Vital{Key: key, Unit: "min", Color: color}
	if v != nil {
		out.Value = fmt.Sprintf("%d", *v)
		out.Fraction = float64(*v) / float64(goal)
	}
	return out
}

// streakVital fills its arc a week at a time: a streak has no goal, but a
// gauge that is always full says nothing.
func streakVital(days int) daypages.Vital {
	return daypages.Vital{
		Key: "streak", Value: fmt.Sprintf("%d", days), Unit: "d",
		Fraction: float64(days%7) / 7, Color: "var(--color-ember)",
	}
}

func foodCard(f day.Food) daypages.FoodCard {
	frac := func(v, goal float64) float64 {
		if goal <= 0 {
			return 0
		}
		return v / goal
	}
	return daypages.FoodCard{
		Calories: fmt.Sprintf("%.0f", f.Calories),
		HasGoal:  f.HasGoal,
		Goal:     fmt.Sprintf("%.0f", f.CalorieGoal),
		Macros: []daypages.Macro{
			{Key: "protein", Letter: "P", Grams: fmt.Sprintf("%.0f", f.ProteinG), Goal: fmt.Sprintf("%.0f", f.ProteinGoalG), Fraction: frac(f.ProteinG, f.ProteinGoalG), Color: "var(--color-day-protein)", Radius: 33},
			{Key: "carb", Letter: "C", Grams: fmt.Sprintf("%.0f", f.CarbG), Goal: fmt.Sprintf("%.0f", f.CarbGoalG), Fraction: frac(f.CarbG, f.CarbGoalG), Color: "var(--color-day-carb)", Radius: 24},
			{Key: "fat", Letter: "F", Grams: fmt.Sprintf("%.0f", f.FatG), Goal: fmt.Sprintf("%.0f", f.FatGoalG), Fraction: frac(f.FatG, f.FatGoalG), Color: "var(--color-day-fat)", Radius: 15},
		},
	}
}

func ring(key string, r day.Ring, unit, color string, radius int) daypages.Ring {
	return daypages.Ring{
		Key: key, Value: fmt.Sprintf("%.0f", r.Value), Goal: fmt.Sprintf("%.0f", r.Goal), Unit: unit,
		Fraction: r.Fraction(), Color: color, Radius: radius,
	}
}

var stageColors = map[string]string{
	string(day.StageAwake): "var(--color-day-move)",
	string(day.StageREM):   "var(--color-day-stand)",
	string(day.StageCore):  "var(--color-day-water)",
	string(day.StageDeep):  "var(--color-day-sleep)",
}

func sleepCard(s *day.Sleep) daypages.SleepCard {
	if s == nil {
		return daypages.SleepCard{}
	}
	out := daypages.SleepCard{
		Logged:    true,
		Hours:     fmt.Sprintf("%d", s.TotalMinutes/60),
		Minutes:   fmt.Sprintf("%d", s.TotalMinutes%60),
		HasStages: s.HasStages(),
	}
	if s.Quality != nil {
		out.Quality = fmt.Sprintf("%d/5", *s.Quality)
	}
	if s.Start != nil && s.End != nil {
		out.Span = s.Start.Format("15:04") + " – " + s.End.Format("15:04")
	}
	if s.HasStages() {
		for _, st := range []day.SleepStage{day.StageDeep, day.StageREM, day.StageCore, day.StageAwake} {
			out.Stages = append(out.Stages, daypages.SleepStageRow{
				Key: string(st), Minutes: fmt.Sprintf("%d", s.StageMinutes[st]), Color: stageColors[string(st)],
			})
		}
		blocks := make([]daypages.SleepBlockInput, len(s.Blocks))
		for i, b := range s.Blocks {
			blocks[i] = daypages.SleepBlockInput{Stage: string(b.Stage), Start: b.Start, End: b.End}
		}
		out.Bars = daypages.SleepBars(blocks, *s.Start, *s.End, stageColors)
	}
	return out
}

func bodyCard(b day.Body) daypages.BodyCard {
	out := daypages.BodyCard{}
	if b.WeightKg != nil {
		out.HasWeight = true
		out.Weight = fmt.Sprintf("%.1f", *b.WeightKg)
	}
	if b.BMI != nil {
		out.HasBMI = true
		out.BMI = fmt.Sprintf("%.1f", math.Round(*b.BMI*10)/10)
		out.BMICategory = string(b.Category())
	}
	return out
}

func kindColor(k dashboard.EntryKind) string {
	switch k {
	case dashboard.KindFood:
		return "var(--color-day-food)"
	case dashboard.KindHydration:
		return "var(--color-day-water)"
	case dashboard.KindSleep:
		return "var(--color-day-sleep)"
	case dashboard.KindActivity:
		return "var(--color-day-fat)"
	case dashboard.KindCheckIn, dashboard.KindHabit:
		return "var(--color-ember)"
	case dashboard.KindJournal:
		return "var(--color-agent)"
	default:
		return "var(--color-signal)"
	}
}

func litres(ml int) string { return fmt.Sprintf("%.1f", float64(ml)/1000) }
