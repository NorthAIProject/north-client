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
	caffeinecalc "github.com/NorthAIProject/north-client/internal/caffeine/caffeine"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/screentime/screen"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
	"github.com/NorthAIProject/north-client/internal/supplements/supplement"
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

	data.Level = s.Level
	data.Vitals = []daypages.Vital{
		{
			Key: "caffeine", Value: fmt.Sprintf("%d", s.Caffeine.ActiveMG), Unit: "mg",
			Fraction: float64(s.Caffeine.TotalMG) / float64(max(s.Caffeine.LimitMG, 1)), Color: "var(--color-day-caffeine)",
		},
		percentVital("energy", s.EnergyPercent, "var(--color-day-move)"),
		minutesVital("sunlight", s.DaylightMinutes, 60, "var(--color-day-sun)"),
		screenVital(s.ScreenMinutes),
		trackerVital(s.Milestones, s.Streak),
	}
	data.Caffeine = daypages.CaffeineCard{
		Total: fmt.Sprintf("%d", s.Caffeine.TotalMG), Active: fmt.Sprintf("%d", s.Caffeine.ActiveMG),
		Limit: fmt.Sprintf("%d", s.Caffeine.LimitMG), AfterCutoff: s.Caffeine.AfterCutoff,
	}
	data.Fast = fastCard(s.Fast, s.IsToday)
	data.Nutrients = daypages.NutrientsCard{Covered: len(s.Nutrients.Covered), Total: s.Nutrients.Total(), Missing: s.Nutrients.Missing}
	for _, m := range s.Milestones {
		data.Milestones = append(data.Milestones, daypages.MilestoneRow{ID: m.ID, Name: m.Name, Months: fmt.Sprintf("%d", m.MonthsSince), Due: m.Due})
	}
	for _, p := range caffeinecalc.Presets() {
		data.CaffeinePresets = append(data.CaffeinePresets, daypages.Preset{Key: p.Key, Value: fmt.Sprintf("%d mg", p.MG)})
	}
	for _, p := range supplement.Presets() {
		data.SupplementPresets = append(data.SupplementPresets, daypages.Preset{Key: p.Key, Label: p.Name})
	}
	data.Regions = sore.Regions()

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
		items = append(items, daypages.RailInput{At: e.At, Title: railTitle(ctx, e), Detail: e.Detail, Href: e.Href, Color: kindColor(e.Kind)})
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

func screenVital(minutes *int) daypages.Vital {
	out := daypages.Vital{Key: "screen", Unit: "", Color: "var(--color-day-screen)"}
	if minutes != nil {
		out.Value = day.FormatMinutes(*minutes)
		out.Fraction = float64(*minutes) / screen.DailyLimitMinutes
	}
	return out
}

// trackerVital is the first "months since" tracker, named by the person; with
// none set up, the slot shows the streak so the strip is never a hole.
func trackerVital(list []day.Milestone, streak int) daypages.Vital {
	if len(list) == 0 {
		return daypages.Vital{
			Key: "streak", Value: fmt.Sprintf("%d", streak), Unit: "d",
			// A streak has no goal, so the arc fills a week at a time.
			Fraction: float64(streak%7) / 7, Color: "var(--color-ember)",
		}
	}
	m := list[0]
	color := "var(--color-day-stand)"
	if m.Due {
		color = "var(--color-day-move)"
	}
	return daypages.Vital{Key: "tracker", Label: m.Name, Value: fmt.Sprintf("%d", m.MonthsSince), Unit: "mo", Fraction: m.Fraction, Color: color}
}

func fastCard(f *day.Fast, isToday bool) daypages.FastCard {
	if f == nil {
		return daypages.FastCard{}
	}
	mins := int(f.Elapsed.Minutes())
	return daypages.FastCard{
		Active:   f.Open() && isToday,
		Shown:    true,
		Elapsed:  fmt.Sprintf("%d:%02d", mins/60, mins%60),
		Phase:    f.Phase,
		Target:   fmt.Sprintf("%d", f.TargetHours),
		Fraction: f.Fraction,
		Started:  f.StartedAt.Format("Mon 15:04"),
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
	if b.TargetWeightKg != nil {
		out.Target = fmt.Sprintf("%.1f", *b.TargetWeightKg)
	}
	if d, ok := b.ToGoal(); ok {
		out.HasGoal = true
		out.ToGoal = fmt.Sprintf("%.1f", d)
	}
	if bp := b.BloodPressure; bp != nil {
		out.HasBloodPressure = true
		out.BloodPressure = fmt.Sprintf("%d/%d", bp.Systolic, bp.Diastolic)
	}
	for _, so := range b.Soreness {
		out.Soreness = append(out.Soreness, daypages.SoreRegion{Region: so.Region, Severity: so.Severity})
	}
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

// railTitle names a drink logged from a preset in the reader's language; the
// preset key is what was stored, so every language can name it.
func railTitle(ctx context.Context, e dashboard.Entry) string {
	if e.Kind == KindCaffeine {
		if _, ok := caffeinecalc.PresetMG(e.Title); ok {
			return i18n.T(ctx, "day.caffeine."+e.Title)
		}
	}
	return e.Title
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
	case KindCaffeine:
		return "var(--color-day-caffeine)"
	case KindSupplement:
		return "var(--color-day-protein)"
	case KindFasting:
		return "var(--color-day-fat)"
	default:
		return "var(--color-signal)"
	}
}

func litres(ml int) string { return fmt.Sprintf("%.1f", float64(ml)/1000) }
