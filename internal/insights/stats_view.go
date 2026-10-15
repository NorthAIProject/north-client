package insights

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func hm(minutes int) string {
	if minutes <= 0 {
		return "0m"
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
}

func pace(seconds float64) string {
	if seconds <= 0 {
		return "—"
	}
	s := int(math.Round(seconds))
	return fmt.Sprintf("%d:%02d /km", s/60, s%60)
}

func duration(seconds float64) string {
	s := int(math.Round(seconds))
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// plural is the catalogue string for n: key+".one" for one, key+".many"
// otherwise, each formatted with n as its first argument.
func plural(ctx context.Context, key string, n int) string {
	if n == 1 {
		return i18n.Tf(ctx, key+".one", n)
	}
	return i18n.Tf(ctx, key+".many", n)
}

func pct(share float64) int { return int(math.Round(share * 100)) }

func buildSleepView(ctx context.Context, rg timerange.Range, st stat.SleepStats) insightpages.SleepView {
	view := insightpages.SleepView{Range: rangeView(ctx, rg), HasData: len(st.Nights) > 0}
	if !view.HasData {
		return view
	}
	nightsNote := i18n.T(ctx, "insights.sleep.nights.many")
	if len(st.Nights) == 1 {
		nightsNote = i18n.T(ctx, "insights.sleep.nights.one")
	}
	view.Tiles = []insightpages.Row{
		{Label: i18n.T(ctx, "insights.sleep.average"), Value: hm(st.AvgMinutes), Note: plural(ctx, "insights.sleep.average.note", len(st.Nights))},
		{Label: i18n.T(ctx, "insights.sleep.debt"), Value: hm(st.DebtMinutes), Note: i18n.T(ctx, "insights.sleep.debt.note")},
		{Label: i18n.T(ctx, "insights.sleep.target"), Value: i18n.Tf(ctx, "insights.stats.nOfM", st.NightsOnTgt, len(st.Nights)), Note: nightsNote},
	}
	if st.HasTimes {
		view.Tiles = append(view.Tiles, insightpages.Row{Label: i18n.T(ctx, "insights.sleep.usualBedtime"), Value: st.AvgBedtime, Note: i18n.Tf(ctx, "insights.sleep.upAt", st.AvgWake)})
	}

	labels := make([]string, len(st.Nights))
	hours := make([]float64, len(st.Nights))
	for i, n := range st.Nights {
		labels[i] = i18n.Date(ctx, n.Date, "Jan 2")
		hours[i] = math.Round(float64(n.Minutes)/6) / 10
	}
	view.Nights = viz.Bar("insights-sleep-nights", i18n.T(ctx, "insights.sleep.chart.hours"), labels, hours)

	if st.HasTimes {
		view.Schedule = append(view.Schedule,
			insightpages.Row{Label: i18n.T(ctx, "insights.sleep.bedtime"), Value: st.AvgBedtime, Note: i18n.Tf(ctx, "insights.sleep.varies", hm(st.BedtimeSpread))},
			insightpages.Row{Label: i18n.T(ctx, "insights.sleep.wake"), Value: st.AvgWake, Note: i18n.Tf(ctx, "insights.sleep.varies", hm(st.WakeSpread))},
		)
	}
	if st.WeekdayAvg > 0 {
		view.Schedule = append(view.Schedule, insightpages.Row{Label: i18n.T(ctx, "insights.stats.weekdays"), Value: hm(st.WeekdayAvg)})
	}
	if st.WeekendAvg > 0 {
		view.Schedule = append(view.Schedule, insightpages.Row{Label: i18n.T(ctx, "insights.stats.weekends"), Value: hm(st.WeekendAvg)})
	}

	for _, stage := range []string{"deep", "rem", "core"} {
		if share, ok := st.StageShare[stage]; ok {
			label := i18n.T(ctx, "insights.sleep.stage."+stage)
			view.Stages = append(view.Stages, insightpages.Bar{Label: label, Value: fmt.Sprintf("%d%%", pct(share)), Pct: pct(share)})
		}
	}
	if st.Best != nil {
		view.Extremes = append(view.Extremes, insightpages.Row{Label: i18n.T(ctx, "insights.sleep.longest"), Value: hm(st.Best.Minutes), Note: i18n.Date(ctx, st.Best.Date, "Mon, Jan 2")})
	}
	if st.Worst != nil {
		view.Extremes = append(view.Extremes, insightpages.Row{Label: i18n.T(ctx, "insights.sleep.shortest"), Value: hm(st.Worst.Minutes), Note: i18n.Date(ctx, st.Worst.Date, "Mon, Jan 2")})
	}
	return view
}

func buildCardioView(ctx context.Context, rg timerange.Range, st stats.CardioStats) insightpages.CardioView {
	view := insightpages.CardioView{Range: rangeView(ctx, rg), HasData: st.Sessions > 0}
	view.Tiles = []insightpages.Row{
		{Label: i18n.T(ctx, "insights.cardio.sessions"), Value: formatCountInt(st.Sessions)},
		{Label: i18n.T(ctx, "insights.cardio.time"), Value: hm(st.Seconds / 60)},
	}
	if st.DistanceKm > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{Label: i18n.T(ctx, "insights.cardio.distance"), Value: fmt.Sprintf("%.1f km", st.DistanceKm)})
	}
	if st.Kcal > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{Label: i18n.T(ctx, "insights.cardio.burned"), Value: fmt.Sprintf("%.0f kcal", st.Kcal)})
	}
	// Distance when there is some; without it (a timer session, a class)
	// time is what there is to chart.
	weekly, unit := st.WeeklyKm, "km"
	view.WeeklyTitle = i18n.T(ctx, "insights.cardio.weekly.distance")
	if st.DistanceKm == 0 {
		weekly, unit = st.WeeklyMins, "min"
		view.WeeklyTitle = i18n.T(ctx, "insights.cardio.weekly.time")
	}
	labels := make([]string, len(weekly))
	values := make([]float64, len(weekly))
	for i, w := range weekly {
		labels[i] = i18n.Date(ctx, w.Day, "Jan 2")
		values[i] = w.Value
	}
	view.Weekly = viz.Bar("insights-cardio-weekly", unit, labels, values)

	most := 1
	for _, k := range st.ByKind {
		most = max(most, k.Seconds)
	}
	for _, k := range st.ByKind {
		value := hm(k.Seconds / 60)
		if k.DistanceKm > 0 {
			value += fmt.Sprintf(" · %.1f km", k.DistanceKm)
		}
		view.Kinds = append(view.Kinds, insightpages.Bar{Label: fmt.Sprintf("%s (%d)", k.Name, k.Sessions), Value: value, Pct: k.Seconds * 100 / most})
	}

	if r := st.Runs; r.Count > 0 {
		view.HasRuns = true
		view.Runs = []insightpages.Row{{Label: i18n.T(ctx, "insights.cardio.runs"), Value: formatCountInt(r.Count)}}
		if r.DistanceKm > 0 {
			view.Runs = append(view.Runs,
				insightpages.Row{Label: i18n.T(ctx, "insights.cardio.distance"), Value: fmt.Sprintf("%.1f km", r.DistanceKm)},
				insightpages.Row{Label: i18n.T(ctx, "insights.cardio.longest"), Value: fmt.Sprintf("%.1f km", r.LongestKm)},
			)
		}
		if r.AvgPace > 0 {
			view.Runs = append(view.Runs,
				insightpages.Row{Label: i18n.T(ctx, "insights.cardio.avgPace"), Value: pace(r.AvgPace)},
				insightpages.Row{Label: i18n.T(ctx, "insights.cardio.bestPace"), Value: pace(r.BestPace)},
			)
		} else {
			view.Runs = append(view.Runs, insightpages.Row{Label: i18n.T(ctx, "insights.cardio.pace"), Value: "—", Note: i18n.T(ctx, "insights.cardio.pace.note")})
		}
		if r.Best5K > 0 {
			view.Runs = append(view.Runs, insightpages.Row{Label: i18n.T(ctx, "insights.cardio.best5k"), Value: duration(r.Best5K), Note: i18n.T(ctx, "insights.cardio.best5k.note")})
		}
	}
	for _, s := range st.Recent {
		value := hm(s.Seconds / 60)
		if s.DistanceM > 0 {
			value = fmt.Sprintf("%.1f km · %s", s.DistanceM/1000, value)
		}
		note := i18n.Date(ctx, s.At, "Mon, Jan 2")
		if p := s.PaceSeconds(); p > 0 && s.IsRun() {
			note += " · " + pace(p)
		}
		view.Recent = append(view.Recent, insightpages.Row{Label: s.Name, Value: value, Note: note})
	}
	for _, h := range []struct {
		label, unit string
		values      []stat.DayValue
		decimals    int
	}{
		{i18n.T(ctx, "insights.cardio.restingHR"), "bpm", st.RestingHR, 0},
		{i18n.T(ctx, "insights.cardio.hrv"), "ms", st.HRV, 0},
		{i18n.T(ctx, "insights.cardio.vo2max"), "", st.VO2Max, 1},
	} {
		if len(h.values) == 0 {
			continue
		}
		last := h.values[len(h.values)-1].Value
		row := insightpages.Row{Label: h.label, Value: strings.TrimSpace(fmt.Sprintf("%.*f %s", h.decimals, last, h.unit))}
		if len(h.values) > 1 {
			change := last - h.values[0].Value
			row.Note = i18n.Tf(ctx, "insights.cardio.since",
				fmt.Sprintf("%+.*f", h.decimals, change), i18n.Date(ctx, h.values[0].Day, "Jan 2"))
		}
		view.Heart = append(view.Heart, row)
	}
	return view
}

func formatCountInt(n int) string { return fmt.Sprintf("%d", n) }

// slotLabel names a time-of-day slot. An unknown slot reads as nothing, as
// it did when this was a map lookup.
func slotLabel(ctx context.Context, key string) string {
	return translated(ctx, "insights.eating.slot."+key, "")
}

func buildEatingView(ctx context.Context, st stat.EatingStats) insightpages.EatingView {
	view := insightpages.EatingView{HasData: st.DaysLogged > 0}
	if !view.HasData {
		return view
	}
	view.Tiles = []insightpages.Row{
		{Label: i18n.T(ctx, "insights.eating.averageDay"), Value: fmt.Sprintf("%.0f kcal", st.AvgKcal), Note: i18n.Tf(ctx, "insights.eating.macros", st.AvgProtein, st.AvgCarb, st.AvgFat)},
	}
	if st.GoalKcal > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{
			Label: i18n.T(ctx, "insights.eating.onTarget"), Value: i18n.Tf(ctx, "insights.eating.ofDays", st.OnTargetDays, plural(ctx, "insights.eating.days", st.DaysLogged)),
			Note: i18n.Tf(ctx, "insights.eating.onTarget.note", st.GoalKcal),
		})
	}
	if st.GoalProtein > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{
			Label: i18n.T(ctx, "insights.eating.proteinMet"), Value: i18n.Tf(ctx, "insights.eating.ofDays", st.ProteinDays, plural(ctx, "insights.eating.days", st.DaysLogged)),
			Note: i18n.Tf(ctx, "insights.eating.proteinMet.note", st.GoalProtein),
		})
	}
	if st.ProteinPerKg > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{Label: i18n.T(ctx, "insights.eating.proteinPerKg"), Value: fmt.Sprintf("%.1f g", st.ProteinPerKg), Note: i18n.T(ctx, "insights.eating.proteinPerKg.note")})
	}
	for _, s := range st.BySlot {
		view.Slots = append(view.Slots, insightpages.Bar{
			Label: slotLabel(ctx, s.Key), Value: fmt.Sprintf("%d%%", pct(s.Share)), Pct: pct(s.Share),
		})
	}
	for _, f := range st.TopFoods {
		view.Foods = append(view.Foods, insightpages.Row{Label: f.Label, Value: fmt.Sprintf("×%d", f.Count), Note: fmt.Sprintf("%.0f kcal", f.Kcal)})
	}
	view.Notes = []insightpages.Row{
		{Label: i18n.T(ctx, "insights.eating.daysLogged"), Value: formatCountInt(st.DaysLogged)},
		{Label: i18n.T(ctx, "insights.eating.late"), Value: plural(ctx, "insights.eating.days", st.LateDays), Note: i18n.T(ctx, "insights.eating.late.note")},
	}
	if st.WeekdayKcal > 0 && st.WeekendKcal > 0 {
		view.Notes = append(view.Notes,
			insightpages.Row{Label: i18n.T(ctx, "insights.stats.weekdays"), Value: fmt.Sprintf("%.0f kcal", st.WeekdayKcal)},
			insightpages.Row{Label: i18n.T(ctx, "insights.stats.weekends"), Value: fmt.Sprintf("%.0f kcal", st.WeekendKcal)},
		)
	}
	return view
}

func buildPatternsView(ctx context.Context, rg timerange.Range, days int, found []stat.Finding) insightpages.PatternsView {
	view := insightpages.PatternsView{Range: rangeView(ctx, rg), Days: days}
	for _, f := range found {
		most := math.Max(math.Abs(f.A.Mean), math.Abs(f.B.Mean))
		if most == 0 {
			most = 1
		}
		format := func(g stat.Group) insightpages.Bar {
			value := fmt.Sprintf("%.1f%s", g.Mean, f.Unit)
			if f.Unit == "min" {
				value = hm(int(math.Round(g.Mean)))
			}
			return insightpages.Bar{
				Label: i18n.Tf(ctx, "insights.patterns.group", g.Label, g.N), Value: value,
				Pct: int(math.Round(math.Abs(g.Mean) / most * 100)),
			}
		}
		view.Findings = append(view.Findings, insightpages.PatternView{Title: f.Title, Detail: f.Detail, A: format(f.A), B: format(f.B)})
	}
	return view
}
