package insights

import (
	"fmt"
	"math"
	"strings"

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

// plural is n and the noun, singular for one.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func pct(share float64) int { return int(math.Round(share * 100)) }

func buildSleepView(rg timerange.Range, st stat.SleepStats) insightpages.SleepView {
	view := insightpages.SleepView{Range: rangeView(rg), HasData: len(st.Nights) > 0}
	if !view.HasData {
		return view
	}
	view.Tiles = []insightpages.Row{
		{Label: "Average", Value: hm(st.AvgMinutes), Note: "over " + plural(len(st.Nights), "night", "nights")},
		{Label: "Sleep debt", Value: hm(st.DebtMinutes), Note: "short of 8 hours, last 7 nights"},
		{Label: "8 hours or more", Value: fmt.Sprintf("%d of %d", st.NightsOnTgt, len(st.Nights)), Note: map[bool]string{true: "night", false: "nights"}[len(st.Nights) == 1]},
	}
	if st.HasTimes {
		view.Tiles = append(view.Tiles, insightpages.Row{Label: "Usual bedtime", Value: st.AvgBedtime, Note: "up at " + st.AvgWake})
	}

	labels := make([]string, len(st.Nights))
	hours := make([]float64, len(st.Nights))
	for i, n := range st.Nights {
		labels[i] = n.Date.Format("Jan 2")
		hours[i] = math.Round(float64(n.Minutes)/6) / 10
	}
	view.Nights = viz.Bar("insights-sleep-nights", "hours", labels, hours)

	if st.HasTimes {
		view.Schedule = append(view.Schedule,
			insightpages.Row{Label: "Bedtime", Value: st.AvgBedtime, Note: fmt.Sprintf("varies by ±%s", hm(st.BedtimeSpread))},
			insightpages.Row{Label: "Wake time", Value: st.AvgWake, Note: fmt.Sprintf("varies by ±%s", hm(st.WakeSpread))},
		)
	}
	if st.WeekdayAvg > 0 {
		view.Schedule = append(view.Schedule, insightpages.Row{Label: "Weekdays", Value: hm(st.WeekdayAvg)})
	}
	if st.WeekendAvg > 0 {
		view.Schedule = append(view.Schedule, insightpages.Row{Label: "Weekends", Value: hm(st.WeekendAvg)})
	}

	for _, stage := range []string{"deep", "rem", "core"} {
		if share, ok := st.StageShare[stage]; ok {
			label := map[string]string{"deep": "Deep", "rem": "REM", "core": "Core"}[stage]
			view.Stages = append(view.Stages, insightpages.Bar{Label: label, Value: fmt.Sprintf("%d%%", pct(share)), Pct: pct(share)})
		}
	}
	if st.Best != nil {
		view.Extremes = append(view.Extremes, insightpages.Row{Label: "Longest", Value: hm(st.Best.Minutes), Note: st.Best.Date.Format("Mon, Jan 2")})
	}
	if st.Worst != nil {
		view.Extremes = append(view.Extremes, insightpages.Row{Label: "Shortest", Value: hm(st.Worst.Minutes), Note: st.Worst.Date.Format("Mon, Jan 2")})
	}
	return view
}

func buildCardioView(rg timerange.Range, st stats.CardioStats) insightpages.CardioView {
	view := insightpages.CardioView{Range: rangeView(rg), HasData: st.Sessions > 0}
	view.Tiles = []insightpages.Row{
		{Label: "Sessions", Value: formatCountInt(st.Sessions)},
		{Label: "Time", Value: hm(st.Seconds / 60)},
	}
	if st.DistanceKm > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{Label: "Distance", Value: fmt.Sprintf("%.1f km", st.DistanceKm)})
	}
	if st.Kcal > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{Label: "Burned", Value: fmt.Sprintf("%.0f kcal", st.Kcal)})
	}
	// Distance when there is some; without it (a timer session, a class)
	// time is what there is to chart.
	weekly, unit := st.WeeklyKm, "km"
	view.WeeklyTitle = "Distance per week"
	if st.DistanceKm == 0 {
		weekly, unit = st.WeeklyMins, "min"
		view.WeeklyTitle = "Time per week"
	}
	labels := make([]string, len(weekly))
	values := make([]float64, len(weekly))
	for i, w := range weekly {
		labels[i] = w.Day.Format("Jan 2")
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
		view.Runs = []insightpages.Row{{Label: "Runs", Value: formatCountInt(r.Count)}}
		if r.DistanceKm > 0 {
			view.Runs = append(view.Runs,
				insightpages.Row{Label: "Distance", Value: fmt.Sprintf("%.1f km", r.DistanceKm)},
				insightpages.Row{Label: "Longest", Value: fmt.Sprintf("%.1f km", r.LongestKm)},
			)
		}
		if r.AvgPace > 0 {
			view.Runs = append(view.Runs,
				insightpages.Row{Label: "Average pace", Value: pace(r.AvgPace)},
				insightpages.Row{Label: "Best pace", Value: pace(r.BestPace)},
			)
		} else {
			view.Runs = append(view.Runs, insightpages.Row{Label: "Pace", Value: "—", Note: "needs a run with a distance"})
		}
		if r.Best5K > 0 {
			view.Runs = append(view.Runs, insightpages.Row{Label: "Best 5K", Value: duration(r.Best5K), Note: "at the pace of your fastest 5K+ run"})
		}
	}
	for _, s := range st.Recent {
		value := hm(s.Seconds / 60)
		if s.DistanceM > 0 {
			value = fmt.Sprintf("%.1f km · %s", s.DistanceM/1000, value)
		}
		note := s.At.Format("Mon, Jan 2")
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
		{"Resting heart rate", "bpm", st.RestingHR, 0},
		{"HRV", "ms", st.HRV, 0},
		{"VO2 max", "", st.VO2Max, 1},
	} {
		if len(h.values) == 0 {
			continue
		}
		last := h.values[len(h.values)-1].Value
		row := insightpages.Row{Label: h.label, Value: strings.TrimSpace(fmt.Sprintf("%.*f %s", h.decimals, last, h.unit))}
		if len(h.values) > 1 {
			change := last - h.values[0].Value
			row.Note = fmt.Sprintf("%+.*f since %s", h.decimals, change, h.values[0].Day.Format("Jan 2"))
		}
		view.Heart = append(view.Heart, row)
	}
	return view
}

func formatCountInt(n int) string { return fmt.Sprintf("%d", n) }

var slotLabels = map[string]string{
	"morning": "Morning (before 11)", "midday": "Midday (11–15)", "afternoon": "Afternoon (15–18)",
	"evening": "Evening (18–21)", "late": "Late (after 21)",
}

func buildEatingView(st stat.EatingStats) insightpages.EatingView {
	view := insightpages.EatingView{HasData: st.DaysLogged > 0}
	if !view.HasData {
		return view
	}
	view.Tiles = []insightpages.Row{
		{Label: "Average day", Value: fmt.Sprintf("%.0f kcal", st.AvgKcal), Note: fmt.Sprintf("P %.0f · C %.0f · F %.0f g", st.AvgProtein, st.AvgCarb, st.AvgFat)},
	}
	if st.GoalKcal > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{
			Label: "On target", Value: fmt.Sprintf("%d of %s", st.OnTargetDays, plural(st.DaysLogged, "day", "days")),
			Note: fmt.Sprintf("within 10%% of %.0f kcal", st.GoalKcal),
		})
	}
	if st.GoalProtein > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{
			Label: "Protein goal met", Value: fmt.Sprintf("%d of %s", st.ProteinDays, plural(st.DaysLogged, "day", "days")),
			Note: fmt.Sprintf("goal %.0f g", st.GoalProtein),
		})
	}
	if st.ProteinPerKg > 0 {
		view.Tiles = append(view.Tiles, insightpages.Row{Label: "Protein per kg", Value: fmt.Sprintf("%.1f g", st.ProteinPerKg), Note: "1.6–2.2 g supports muscle gain"})
	}
	for _, s := range st.BySlot {
		view.Slots = append(view.Slots, insightpages.Bar{
			Label: slotLabels[s.Key], Value: fmt.Sprintf("%d%%", pct(s.Share)), Pct: pct(s.Share),
		})
	}
	for _, f := range st.TopFoods {
		view.Foods = append(view.Foods, insightpages.Row{Label: f.Label, Value: fmt.Sprintf("×%d", f.Count), Note: fmt.Sprintf("%.0f kcal", f.Kcal)})
	}
	view.Notes = []insightpages.Row{
		{Label: "Days logged", Value: formatCountInt(st.DaysLogged)},
		{Label: "Late eating", Value: plural(st.LateDays, "day", "days"), Note: "last food after your kitchen closes"},
	}
	if st.WeekdayKcal > 0 && st.WeekendKcal > 0 {
		view.Notes = append(view.Notes,
			insightpages.Row{Label: "Weekdays", Value: fmt.Sprintf("%.0f kcal", st.WeekdayKcal)},
			insightpages.Row{Label: "Weekends", Value: fmt.Sprintf("%.0f kcal", st.WeekendKcal)},
		)
	}
	return view
}

func buildPatternsView(rg timerange.Range, days int, found []stat.Finding) insightpages.PatternsView {
	view := insightpages.PatternsView{Range: rangeView(rg), Days: days}
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
				Label: fmt.Sprintf("%s (%d days)", g.Label, g.N), Value: value,
				Pct: int(math.Round(math.Abs(g.Mean) / most * 100)),
			}
		}
		view.Findings = append(view.Findings, insightpages.PatternView{Title: f.Title, Detail: f.Detail, A: format(f.A), B: format(f.B)})
	}
	return view
}
