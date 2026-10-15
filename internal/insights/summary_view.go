package insights

import (
	"context"
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/insights/highlight"
	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// domain names a scored area for the summary: which catalogue key names it,
// which icon it wears, and which detail page its card opens.
type domain struct {
	Key  string
	Icon string
	Href string
}

// Label is the domain's name in the reader's language. The section keys are
// shared with the sub-nav, so a ring and its tab cannot be called different
// things.
func (d domain) Label(ctx context.Context) string {
	return i18n.T(ctx, "insights.section."+d.Key)
}

// domains is the order the rings appear in, chosen to read as a day does:
// what the body did, how it felt, what it was for, what was trained.
var domains = []domain{
	{Key: "body", Icon: "heart-pulse", Href: "/app/insights/body"},
	{Key: "mind", Icon: "brain", Href: "/app/insights/mind"},
	{Key: "progress", Icon: "target", Href: "/app/insights/progress"},
	{Key: "training", Icon: "dumbbell", Href: "/app/insights/training"},
	{Key: "nutrition", Icon: "utensils", Href: "/app/insights/nutrition"},
}

// componentLabel turns a score component's key into something a person reads.
// Without it a breakdown row renders the identifier, which is a leak; a key
// the catalogue does not know yet renders as nothing, as it always has.
func componentLabel(ctx context.Context, key string) string {
	return translated(ctx, "insights.component."+key, "")
}

// verdictWord is the word beside the number.
func verdictWord(ctx context.Context, v score.Verdict) string {
	return translated(ctx, "insights.verdict."+string(v), "")
}

// onTrackFrom is the verdict at which a domain counts toward "n of m on track".
const onTrackFrom = 70

// maxHighlights is how many sentences the section shows. Three is what fits
// without the block becoming something people scroll past.
const maxHighlights = 3

// buildSummaryView assembles the rings, the pinned cards and the counts.
func buildSummaryView(ctx context.Context, data SummaryData) (insightpages.SummaryView, error) {
	scores := summaryScores(data)

	view := insightpages.SummaryView{Range: rangeView(ctx, data.Range)}
	for i, d := range domains {
		card, err := scoreCard(ctx, d, scores[i])
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

	view.Pinned = pinnedCards(ctx, data)
	view.Days, view.HasDays = dayTable(ctx, data)
	view.Highlights = highlight.Find(summaryHighlights(ctx, data), maxHighlights)

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
func pinnedCards(ctx context.Context, data SummaryData) []insightpages.PinnedCard {
	loc := data.Range.Location()
	labels := bucketLabels(ctx, data.Range)
	var out []insightpages.PinnedCard

	if total, points := hydrationSeries(data, loc); total > 0 {
		out = append(out, insightpages.PinnedCard{
			Label: i18n.T(ctx, "insights.pinned.water"), Icon: "droplet", Href: metricHref("water"),
			Value:    formatLitres(ctx, total),
			Note:     i18n.Tf(ctx, "insights.pinned.water.note", len(data.Body.Hydration)),
			Chart:    viz.Sparkline("insights-pinned-water", labels, points),
			HasChart: true,
		})
	}

	if n := len(data.Body.Nights); n > 0 {
		nightly := bucketedMean(data.Range, sleepPoints(data, loc))
		out = append(out, insightpages.PinnedCard{
			Label: i18n.T(ctx, "insights.pinned.sleep"), Icon: "moon", Href: metricHref("sleep"),
			Value:    formatHours(data.Body.SleepTrend.AverageMinutes),
			Note:     i18n.Tf(ctx, "insights.pinned.sleep.note", n),
			Chart:    viz.Sparkline("insights-pinned-sleep", labels, nightly),
			HasChart: true,
		})
	}

	if n := len(data.Mind.CheckIns); n > 0 {
		out = append(out, insightpages.PinnedCard{
			Label: i18n.T(ctx, "insights.pinned.checkins"), Icon: "smile", Href: metricHref("checkins"),
			Value:    fmt.Sprintf("%d", n),
			Note:     i18n.Tf(ctx, "insights.pinned.checkins.note", data.Progress.Streak),
			Chart:    viz.Sparkline("insights-pinned-checkins", labels, checkInPoints(data, loc)),
			HasChart: true,
		})
	}

	if n := len(data.Training.Sessions); n > 0 {
		out = append(out, insightpages.PinnedCard{
			Label: i18n.T(ctx, "insights.pinned.training"), Icon: "dumbbell", Href: metricHref("sessions"),
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

func scoreCard(ctx context.Context, d domain, s score.Score) (insightpages.ScoreCard, error) {
	label := d.Label(ctx)
	card := insightpages.ScoreCard{
		Key:      d.Key,
		Label:    label,
		Icon:     d.Icon,
		Href:     d.Href,
		Points:   s.Points,
		Verdict:  verdictWord(ctx, s.Verdict()),
		Coverage: s.Coverage,
		HasData:  s.HasData,
	}

	for _, c := range s.Components {
		card.Components = append(card.Components, insightpages.ComponentRow{
			Label:  componentLabel(ctx, c.Key),
			Earned: c.Earned,
			Weight: c.Weight,
			Known:  c.Known,
		})
	}

	if r, ok := s.Reason(); ok {
		best, worst := componentLabel(ctx, r.Best), componentLabel(ctx, r.Worst)
		// Both cases are passed so each language picks what its sentence
		// needs: English keeps the labels as they stand mid-sentence.
		card.Reason = i18n.Tf(ctx, "insights.score.reason",
			best, worst, lowerFirst(best), lowerFirst(worst))
	}

	if s.HasData {
		opt, err := option(viz.GaugeOptionJSON(label, s.Points))
		if err != nil {
			return insightpages.ScoreCard{}, err
		}
		card.Option = opt
	}
	return card, nil
}

// lowerFirst makes a label fit mid-sentence without lowercasing an acronym.
func lowerFirst(label string) string {
	first, size := utf8.DecodeRuneInString(label)
	if first == utf8.RuneError {
		return label
	}
	for _, r := range label[size:] {
		if unicode.IsUpper(r) {
			return label
		}
	}
	return string(unicode.ToLower(first)) + label[size:]
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

func formatLitres(ctx context.Context, ml int) string {
	if ml <= 0 {
		return "—"
	}
	if ml >= 1000 {
		return i18n.Decimal(ctx, float64(ml)/1000, 1) + " L"
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
func summaryHighlights(ctx context.Context, data SummaryData) highlight.Input {
	loc := data.Range.Location()
	labels := bucketLabels(ctx, data.Range)
	period := periodNoun(data.Range)

	in := highlight.Input{Locale: i18n.LocaleFrom(ctx)}

	if len(data.Body.Nights) > 0 {
		in.Series = append(in.Series, highlight.Series{
			Label: i18n.T(ctx, "insights.series.sleep"), Unit: "h", Decimals: 1, Period: period, Labels: labels,
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
			Label: i18n.T(ctx, "insights.series.water"), Unit: "L", Decimals: 1, Period: period, Labels: labels, Values: litres,
		})
	}

	if len(data.Training.Sessions) > 0 {
		in.Series = append(in.Series, highlight.Series{
			Label: i18n.T(ctx, "insights.series.burn"), Unit: "kcal", Decimals: 0, Period: period, Labels: labels,
			Values: burnSeries(data, loc),
		})
		in.Comparisons = append(in.Comparisons, highlight.Comparison{
			Label: i18n.T(ctx, "insights.series.burn"), Current: data.Training.Calories, Prior: data.Training.Prior,
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
func dayTable(ctx context.Context, data SummaryData) ([]insightpages.DayRow, bool) {
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
			Label: bucketLabel(ctx, data.Range, b),
			Cells: []insightpages.DayCell{
				{Label: i18n.T(ctx, "insights.days.sleep"), Value: i18n.Decimal(ctx, sleep[i], 1) + "h", Filled: sleep[i] > 0},
				{Label: i18n.T(ctx, "insights.days.water"), Value: formatLitres(ctx, int(water[i])), Filled: water[i] > 0},
				{Label: i18n.T(ctx, "insights.days.checkin"), Value: i18n.T(ctx, "insights.days.yes"), Filled: checkIns[i] > 0},
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
