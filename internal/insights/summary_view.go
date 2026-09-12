package insights

import (
	"fmt"
	"time"

	"github.com/NorthAIProject/north-client/internal/insights/highlight"
	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// domain names a scored area for the summary: what to call it, which icon it
// wears, and which detail page its card opens.
type domain struct {
	Key   string
	Label string
	Icon  string
	Href  string
}

// domains is the order the rings appear in, chosen to read as a day does:
// what the body did, how it felt, what it was for, what was trained.
var domains = []domain{
	{Key: "body", Label: "Body", Icon: "heart-pulse", Href: "/app/insights/body"},
	{Key: "mind", Label: "Mind", Icon: "brain", Href: "/app/insights/mind"},
	{Key: "progress", Label: "Progress", Icon: "target", Href: "/app/insights/progress"},
	{Key: "training", Label: "Training", Icon: "dumbbell", Href: "/app/insights/training"},
	{Key: "nutrition", Label: "Nutrition", Icon: "utensils", Href: "/app/insights/nutrition"},
}

// componentLabels turns a score component's key into something a person reads.
// Without it a breakdown row renders the identifier, which is a leak.
var componentLabels = map[string]string{
	"sleep_duration":      "Sleep duration",
	"bedtime_consistency": "Bedtime",
	"hydration":           "Water",
	"habits":              "Habits",
	"checkin_coverage":    "Check-ins",
	"mood":                "Mood",
	"journal":             "Journal",
	"notes":               "Goal notes",
	"overdue":             "Milestones",
	"streak":              "Streak",
	"frequency":           "Sessions",
	"volume":              "Burn",
	"consistency":         "Spread",
	"calories":            "Calories",
	"protein":             "Protein",
	"food_logging":        "Meals logged",
}

// verdictWords are the words beside the number.
var verdictWords = map[score.Verdict]string{
	score.VerdictStrong: "Strong",
	score.VerdictOK:     "OK",
	score.VerdictUneven: "Uneven",
	score.VerdictLow:    "Low",
}

// onTrackFrom is the verdict at which a domain counts toward "n of m on track".
const onTrackFrom = 70

// maxHighlights is how many sentences the section shows. Three is what fits
// without the block becoming something people scroll past.
const maxHighlights = 3

// buildSummaryView assembles the rings, the pinned cards and the counts.
func buildSummaryView(data SummaryData) (insightpages.SummaryView, error) {
	scores := summaryScores(data)

	view := insightpages.SummaryView{Range: rangeView(data.Range)}
	for i, d := range domains {
		card, err := scoreCard(d, scores[i])
		if err != nil {
			return insightpages.SummaryView{}, err
		}
		view.Scores = append(view.Scores, card)

		if card.HasData {
			view.Judged++
			if card.Points >= onTrackFrom {
				view.OnTrack++
			}
		}
	}

	view.Pinned = pinnedCards(data)
	view.Days, view.HasDays = dayTable(data)
	view.Highlights = highlight.Find(summaryHighlights(data), maxHighlights)

	// One sentence beats four empty rings and a grid of dashes. Judged is the
	// right test: a window where nothing could be scored is a window where
	// nothing was logged.
	view.Empty = view.Judged == 0 && len(view.Pinned) == 0

	return view, nil
}

// metricHref is the detail page for a metric key. A pinned card opens the
// number it shows rather than the category it belongs to, which is what makes
// tapping one feel like following the number rather than leaving it.
func metricHref(key string) string { return "/app/insights/metric/" + key }

// pinnedCards is the headline number from each domain that has one, with the
// shape of the window beside it.
func pinnedCards(data SummaryData) []insightpages.PinnedCard {
	loc := data.Range.Location()
	labels := bucketLabels(data.Range)
	var out []insightpages.PinnedCard

	if total, points := hydrationSeries(data, loc); total > 0 {
		out = append(out, insightpages.PinnedCard{
			Label: "Water", Icon: "droplet", Href: metricHref("water"),
			Value:    formatLitres(total),
			Note:     fmt.Sprintf("across %d days", len(data.Body.Hydration)),
			Chart:    viz.Sparkline("insights-pinned-water", labels, points),
			HasChart: true,
		})
	}

	if n := len(data.Body.Nights); n > 0 {
		nightly := bucketedMean(data.Range, sleepPoints(data, loc))
		out = append(out, insightpages.PinnedCard{
			Label: "Sleep", Icon: "moon", Href: metricHref("sleep"),
			Value:    formatHours(data.Body.SleepTrend.AverageMinutes),
			Note:     fmt.Sprintf("average of %d nights", n),
			Chart:    viz.Sparkline("insights-pinned-sleep", labels, nightly),
			HasChart: true,
		})
	}

	if n := len(data.Mind.CheckIns); n > 0 {
		out = append(out, insightpages.PinnedCard{
			Label: "Check-ins", Icon: "smile", Href: metricHref("checkins"),
			Value:    fmt.Sprintf("%d", n),
			Note:     fmt.Sprintf("%d day streak", data.Progress.Streak),
			Chart:    viz.Sparkline("insights-pinned-checkins", labels, checkInPoints(data, loc)),
			HasChart: true,
		})
	}

	if n := len(data.Training.Sessions); n > 0 {
		out = append(out, insightpages.PinnedCard{
			Label: "Training", Icon: "dumbbell", Href: metricHref("sessions"),
			Value:    fmt.Sprintf("%d", n),
			Note:     formatKcalSummary(data.Training.Calories),
			Delta:    deltaView(data.Training.Calories, data.Training.Prior),
			Chart:    viz.Sparkline("insights-pinned-burn", labels, burnSeries(data, loc)),
			HasChart: true,
		})
	}

	return out
}

// summaryScores computes every domain in the order the page shows them.
func summaryScores(data SummaryData) []score.Score {
	days := data.Range.Days()

	return []score.Score{
		score.Body(data.Body.Nights, data.Body.Hydration, data.Body.Habits, days),
		score.Mind(data.Mind.CheckIns, data.Mind.Journal, days),
		score.Progress(score.ProgressInput{
			ActiveGoals: len(data.Progress.Active),
			Notes:       len(data.Progress.Notes),
			Overdue:     data.Progress.Overdue,
			Streak:      data.Progress.Streak,
			WindowDays:  days,
		}),
		score.Training(data.Training.Sessions, data.Training.Calories, data.Training.Prior, days),
		score.Nutrition(nutritionInput(data.Nutrition, days)),
	}
}

func scoreCard(d domain, s score.Score) (insightpages.ScoreCard, error) {
	card := insightpages.ScoreCard{
		Key:      d.Key,
		Label:    d.Label,
		Icon:     d.Icon,
		Href:     d.Href,
		Points:   s.Points,
		Verdict:  verdictWords[s.Verdict()],
		Coverage: s.Coverage,
		HasData:  s.HasData,
	}

	for _, c := range s.Components {
		card.Components = append(card.Components, insightpages.ComponentRow{
			Label:  componentLabels[c.Key],
			Earned: c.Earned,
			Weight: c.Weight,
			Known:  c.Known,
		})
	}

	if r, ok := s.Reason(); ok {
		card.Reason = fmt.Sprintf("%s carried this; %s cost the most.",
			componentLabels[r.Best], componentLabels[r.Worst])
	}

	if s.HasData {
		opt, err := option(viz.GaugeOptionJSON(d.Label, s.Points))
		if err != nil {
			return insightpages.ScoreCard{}, err
		}
		card.Option = opt
	}
	return card, nil
}

// The series behind each pinned card's sparkline. Each mirrors how its detail
// page buckets the same rows, so the small chart and the big one agree.

func hydrationSeries(data SummaryData, loc *time.Location) (int, []float64) {
	points := make([]point, 0, len(data.Body.Hydration))
	total := 0
	for _, d := range data.Body.Hydration {
		points = append(points, point{At: d.Date.In(loc), Value: float64(d.TotalML)})
		total += d.TotalML
	}
	return total, bucketedMean(data.Range, points)
}

func sleepPoints(data SummaryData, loc *time.Location) []point {
	points := make([]point, 0, len(data.Body.Nights))
	for _, n := range data.Body.Nights {
		points = append(points, point{At: n.LocalDate.In(loc), Value: float64(n.DurationMinutes) / 60})
	}
	return points
}

func checkInPoints(data SummaryData, loc *time.Location) []float64 {
	points := make([]point, 0, len(data.Mind.CheckIns))
	for _, c := range data.Mind.CheckIns {
		points = append(points, point{At: c.LocalDate.In(loc), Value: 1})
	}
	return bucketed(data.Range, points)
}

func burnSeries(data SummaryData, loc *time.Location) []float64 {
	points := make([]point, 0, len(data.Training.Sessions))
	for _, s := range data.Training.Sessions {
		if s.EndedAt == nil || s.CaloriesBurned == nil {
			continue
		}
		points = append(points, point{At: s.EndedAt.In(loc), Value: *s.CaloriesBurned})
	}
	return bucketed(data.Range, points)
}

// The page's own formatters. web/insights has an identical set, unexported and
// reachable only from a template; duplicating four of them here is cheaper
// than exporting a view package's internals to the layer that feeds it. If a
// third caller appears — the Telegram digest will be one — they belong in a
// leaf of their own rather than in a third copy.

func formatLitres(ml int) string {
	if ml <= 0 {
		return "—"
	}
	if ml >= 1000 {
		return fmt.Sprintf("%.1f L", float64(ml)/1000)
	}
	return fmt.Sprintf("%d ml", ml)
}

func formatHours(minutes float64) string {
	if minutes <= 0 {
		return "—"
	}
	h := int(minutes) / 60
	m := int(minutes) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func formatKcalSummary(kcal float64) string {
	if kcal <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f kcal", kcal)
}

// summaryHighlights is the window handed to the rules.
//
// Only series a sentence can sensibly be built about: a peak day of one
// check-in is not an observation, so check-ins are counted on their card and
// left out here. Water is in litres and sleep in hours because the rules
// render the number as it stands, and "2000 ml" reads worse than "2.0L".
func summaryHighlights(data SummaryData) highlight.Input {
	loc := data.Range.Location()
	labels := bucketLabels(data.Range)
	period := periodNoun(data.Range)

	in := highlight.Input{}

	if len(data.Body.Nights) > 0 {
		in.Series = append(in.Series, highlight.Series{
			Label: "Sleep", Unit: "h", Decimals: 1, Period: period, Labels: labels,
			Values: bucketedMean(data.Range, sleepPoints(data, loc)),
		})
	}

	if len(data.Body.Hydration) > 0 {
		_, ml := hydrationSeries(data, loc)
		litres := make([]float64, len(ml))
		for i, v := range ml {
			litres[i] = v / 1000
		}
		in.Series = append(in.Series, highlight.Series{
			Label: "Water", Unit: "L", Decimals: 1, Period: period, Labels: labels, Values: litres,
		})
	}

	if len(data.Training.Sessions) > 0 {
		in.Series = append(in.Series, highlight.Series{
			Label: "Burn", Unit: "kcal", Decimals: 0, Period: period, Labels: labels,
			Values: burnSeries(data, loc),
		})
		in.Comparisons = append(in.Comparisons, highlight.Comparison{
			Label: "Burn", Current: data.Training.Calories, Prior: data.Training.Prior,
		})
	}

	return in
}

// maxDayRows is the longest window the day-by-day table is offered for. A
// month already asks for scrolling; a quarter would be ninety rows nobody
// reads, and the charts above answer the same question at that length.
const maxDayRows = 31

// dayTable lists the window one day at a time, the way the activity view lists
// a week: a mark per metric per day, and a dash where nothing was logged.
func dayTable(data SummaryData) ([]insightpages.DayRow, bool) {
	if data.Range.Grain != timerange.GrainDay {
		return nil, false
	}
	buckets := data.Range.Buckets()
	if len(buckets) == 0 || len(buckets) > maxDayRows {
		return nil, false
	}

	loc := data.Range.Location()
	sleep := bucketedMean(data.Range, sleepPoints(data, loc))
	_, water := hydrationSeries(data, loc)
	checkIns := checkInPoints(data, loc)

	rows := make([]insightpages.DayRow, len(buckets))
	for i, b := range buckets {
		rows[i] = insightpages.DayRow{
			Label: b.Label,
			Cells: []insightpages.DayCell{
				{Label: "Sleep", Value: fmt.Sprintf("%.1fh", sleep[i]), Filled: sleep[i] > 0},
				{Label: "Water", Value: formatLitres(int(water[i])), Filled: water[i] > 0},
				{Label: "Check-in", Value: "Yes", Filled: checkIns[i] > 0},
			},
		}
	}
	return rows, true
}

// nutritionInput flattens the window's food logs into the plain numbers the
// scorer takes. The zero goals of somebody who has not run the calculator
// leave the intake components unknown, which is the honest reading.
func nutritionInput(data NutritionData, windowDays int) score.NutritionInput {
	in := score.NutritionInput{WindowDays: windowDays}
	for _, d := range data.Days {
		in.Days = append(in.Days, score.NutritionDay{
			Calories: d.Macros.Calories,
			ProteinG: d.Macros.ProteinG,
		})
	}
	if data.HasGoal {
		in.CalorieGoal = data.Goal.CalorieGoal
		in.ProteinGoal = data.Goal.ProteinG
	}
	return in
}
