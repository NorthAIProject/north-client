package insights

import (
	"fmt"
	"sort"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// Chart options are built here rather than in a template, the same way
// goals/view.go does it: a templ file that constructs an ECharts option is a
// templ file nobody can test.

func buildTimelineView(data TimelineData) insightpages.TimelineView {
	rows := make([]insightpages.EntryRow, len(data.Entries))
	for i, e := range data.Entries {
		rows[i] = insightpages.EntryRow{
			Kind:   string(e.Kind),
			Label:  e.Kind.Label(),
			At:     e.At,
			Title:  e.Title,
			Detail: e.Detail,
			Href:   e.Href,
			Icon:   e.Icon,
		}
	}

	// Chips in a fixed order rather than map order, so the filter row does not
	// reshuffle itself between renders of the same page.
	kinds := []dashboard.EntryKind{
		dashboard.KindCheckIn,
		dashboard.KindHydration,
		dashboard.KindSleep,
		dashboard.KindHabit,
		dashboard.KindJournal,
		dashboard.KindGoal,
		dashboard.KindGoalNote,
		dashboard.KindActivity,
	}
	chips := make([]insightpages.FilterChip, 0, len(kinds)+1)
	chips = append(chips, insightpages.FilterChip{
		Key: "", Label: "Everything", Count: data.Total, Selected: data.Kind == "",
	})
	for _, k := range kinds {
		n := data.Counts[k]
		if n == 0 {
			continue
		}
		chips = append(chips, insightpages.FilterChip{
			Key: string(k), Label: k.Label(), Count: n, Selected: data.Kind == string(k),
		})
	}

	return insightpages.TimelineView{
		Range:    rangeView(data.Range),
		Entries:  rows,
		Chips:    chips,
		Overflow: data.Overflow,
	}
}

func buildBodyView(data BodyData) (insightpages.BodyView, error) {
	loc := data.Range.Location()
	labels := bucketLabels(data.Range)

	// Both series are daily measurements, so a bucket wider than a day holds
	// their average rather than their sum: a week of eight-hour nights is an
	// eight-hour week, and the axis has to keep its meaning across ranges.
	waterPoints := make([]point, 0, len(data.Hydration))
	totalWaterML := 0
	for _, d := range data.Hydration {
		waterPoints = append(waterPoints, point{At: d.Date.In(loc), Value: float64(d.TotalML)})
		totalWaterML += d.TotalML
	}
	water := bucketedMean(data.Range, waterPoints)

	sleepPoints := make([]point, 0, len(data.Nights))
	for _, n := range data.Nights {
		sleepPoints = append(sleepPoints, point{
			At:    n.LocalDate.In(loc),
			Value: float64(n.DurationMinutes) / 60,
		})
	}
	sleepHours := bucketedMean(data.Range, sleepPoints)

	view := insightpages.BodyView{
		Range:        rangeView(data.Range),
		WaterChart:   viz.Bar("insights-body-water", "Water (ml)", labels, water),
		SleepChart:   viz.SingleLine("insights-body-sleep", "Hours slept", labels, sleepHours, nil, nil),
		TotalWaterML: totalWaterML,
		Nights:       len(data.Nights),
		AvgSleep:     data.SleepTrend.AverageMinutes,
		AvgQuality:   data.SleepTrend.AverageQuality,
		QualityCount: data.SleepTrend.QualityCount,
		HasWater:     anyPositive(water),
		HasSleep:     len(data.Nights) > 0,
	}

	if len(data.Habits) > 0 {
		kept, scheduled := 0, 0
		rows := make([]insightpages.HabitRow, 0, len(data.Habits))
		for _, st := range data.Habits {
			kept += st.Kept
			scheduled += st.Scheduled
			rate := 0
			if st.Scheduled > 0 {
				rate = st.Kept * 100 / st.Scheduled
			}
			rows = append(rows, insightpages.HabitRow{
				Name: st.Habit.Name, Kept: st.Kept, Scheduled: st.Scheduled,
				Streak: st.Streak, Rate: rate,
			})
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Rate > rows[j].Rate })

		overall := 0
		if scheduled > 0 {
			overall = kept * 100 / scheduled
		}
		gauge, err := option(viz.GaugeOptionJSON("Adherence", overall))
		if err != nil {
			return insightpages.BodyView{}, err
		}
		view.Habits = rows
		view.HabitGauge = gauge
		view.HasHabits = true
	}

	return view, nil
}

func buildMindView(data MindData) (insightpages.MindView, error) {
	loc := data.Range.Location()
	buckets := data.Range.Buckets()
	labels := bucketLabels(data.Range)

	mood := make([]int, len(buckets))
	energy := make([]int, len(buckets))
	cells := make([]viz.HeatmapCell, len(buckets))

	for _, c := range data.CheckIns {
		if i := data.Range.Index(buckets, c.LocalDate.In(loc)); i >= 0 {
			mood[i] = c.Mood
			energy[i] = c.Energy
		}
	}

	journalPoints := make([]point, 0, len(data.Journal))
	for _, e := range data.Journal {
		journalPoints = append(journalPoints, point{At: e.CreatedAt.In(loc), Value: 1})
	}
	journalCount := bucketed(data.Range, journalPoints)

	for i, b := range buckets {
		cells[i] = viz.HeatmapCell{Label: b.Label, Value: mood[i]}
	}

	heatmap, err := option(viz.HeatmapJSON("Mood", cells))
	if err != nil {
		return insightpages.MindView{}, err
	}

	avgMood, avgEnergy, rated := averageMoodEnergy(mood, energy)

	return insightpages.MindView{
		Range:        rangeView(data.Range),
		MoodChart:    viz.MoodEnergyLine("insights-mind-mood", labels, mood, energy),
		JournalChart: viz.Bar("insights-mind-journal", "Entries", labels, journalCount),
		MoodHeatmap:  heatmap,
		CheckInCount: rated,
		JournalCount: len(data.Journal),
		AvgMood:      avgMood,
		AvgEnergy:    avgEnergy,
		HasCheckIns:  rated > 0,
		HasJournal:   len(data.Journal) > 0,
	}, nil
}

func buildProgressView(data ProgressData) (insightpages.ProgressView, error) {
	loc := data.Range.Location()
	labels := bucketLabels(data.Range)

	notePoints := make([]point, 0, len(data.Notes))
	for _, n := range data.Notes {
		notePoints = append(notePoints, point{At: n.CreatedAt.In(loc), Value: 1})
	}
	notes := bucketed(data.Range, notePoints)

	var (
		active      int
		progressSum int
		progressN   int
		byStatus    = map[string]int{}
		statusOrder []string
		rows        []insightpages.GoalRow
	)
	for _, g := range data.Active {
		if byStatus[g.Status] == 0 {
			statusOrder = append(statusOrder, g.Status)
		}
		byStatus[g.Status]++

		if !g.IsActive() {
			continue
		}
		active++
		pct, ok := g.Progress()
		if ok {
			progressSum += pct
			progressN++
		}
		rows = append(rows, insightpages.GoalRow{
			ID: g.ID.String(), Title: g.Title, Progress: pct, HasProgress: ok,
			Deadline: g.Deadline(), Overdue: g.Overdue(),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Progress > rows[j].Progress })

	segments := make([]viz.DonutSegment, 0, len(statusOrder))
	for _, st := range statusOrder {
		segments = append(segments, viz.DonutSegment{Label: statusLabel(st), Value: byStatus[st]})
	}
	donut, err := option(viz.DonutOptionJSON(segments))
	if err != nil {
		return insightpages.ProgressView{}, err
	}

	avg := 0
	if progressN > 0 {
		avg = progressSum / progressN
	}

	return insightpages.ProgressView{
		Range:       rangeView(data.Range),
		Goals:       rows,
		NotesChart:  viz.Bar("insights-progress-notes", "Updates", labels, notes),
		StatusDonut: donut,
		HasDonut:    len(segments) > 0,
		ActiveCount: active,
		AvgProgress: avg,
		Overdue:     data.Overdue,
		Streak:      data.Streak,
		NoteCount:   len(data.Notes),
		OpenedCount: len(data.Opened),
		HasNotes:    len(data.Notes) > 0,
	}, nil
}

func buildTrainingView(data TrainingData) (insightpages.TrainingView, error) {
	loc := data.Range.Location()
	labels := bucketLabels(data.Range)
	burnPoints := make([]point, 0, len(data.Sessions))

	var (
		totalSeconds int
		byKind       = map[string]int{}
		kindOrder    []string
		rows         []insightpages.SessionRow
	)
	for _, sess := range data.Sessions {
		if sess.EndedAt == nil {
			continue
		}
		ended := sess.EndedAt.In(loc)
		if sess.CaloriesBurned != nil {
			burnPoints = append(burnPoints, point{At: ended, Value: *sess.CaloriesBurned})
		}

		name := sess.ActivityCode
		if met, ok := activity.LookupMET(sess.ActivityCode); ok {
			name = met.Name
		}
		if byKind[name] == 0 {
			kindOrder = append(kindOrder, name)
		}
		byKind[name]++

		seconds := int(sess.EndedAt.Sub(sess.StartedAt).Seconds()) - sess.TotalPausedSeconds
		if seconds < 0 {
			seconds = 0
		}
		totalSeconds += seconds

		kcal := 0.0
		if sess.CaloriesBurned != nil {
			kcal = *sess.CaloriesBurned
		}
		rows = append(rows, insightpages.SessionRow{
			Name: name, At: ended, Duration: formatDuration(seconds), Calories: kcal,
		})
	}

	// Calories are a total, not a daily measurement, so a wider bucket sums.
	burn := bucketed(data.Range, burnPoints)

	segments := make([]viz.DonutSegment, 0, len(kindOrder))
	for _, k := range kindOrder {
		segments = append(segments, viz.DonutSegment{Label: k, Value: byKind[k]})
	}
	donut, err := option(viz.DonutOptionJSON(segments))
	if err != nil {
		return insightpages.TrainingView{}, err
	}

	return insightpages.TrainingView{
		Range:        rangeView(data.Range),
		Sessions:     rows,
		BurnChart:    viz.Bar("insights-training-burn", "kcal", labels, burn),
		KindDonut:    donut,
		HasDonut:     len(segments) > 0,
		Calories:     data.Calories,
		Delta:        deltaView(data.Calories, data.Prior),
		SessionCount: len(rows),
		TotalTime:    formatDuration(totalSeconds),
		HasSessions:  len(rows) > 0,
	}, nil
}

func rangeView(rg timerange.Range) insightpages.RangeView {
	all := timerange.All(rg.Location())
	options := make([]insightpages.RangeOption, len(all))
	for i, r := range all {
		options[i] = insightpages.RangeOption{Key: r.Key, Label: r.Label, Selected: r.Key == rg.Key}
	}
	return insightpages.RangeView{Key: rg.Key, Label: rg.Label, Options: options}
}

// deltaView mirrors the dashboard's rule: no prior window means no claim.
func deltaView(current, prior float64) insightpages.DeltaView {
	if prior == 0 {
		return insightpages.DeltaView{}
	}
	pct := (current - prior) / prior * 100
	dir := 0
	switch {
	case current > prior:
		dir = 1
	case current < prior:
		dir = -1
	}
	return insightpages.DeltaView{Pct: pct, Direction: dir, HasPrior: true}
}

// option runs an ECharts builder and decodes it in one step, since every
// caller here does exactly that and the two-step form triples the error
// handling.
func option(raw []byte, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	return viz.UnmarshalOption(raw)
}

func averageMoodEnergy(mood, energy []int) (float64, float64, int) {
	var sumMood, sumEnergy, n int
	for i := range mood {
		if mood[i] <= 0 {
			continue
		}
		sumMood += mood[i]
		sumEnergy += energy[i]
		n++
	}
	if n == 0 {
		return 0, 0, 0
	}
	return float64(sumMood) / float64(n), float64(sumEnergy) / float64(n), n
}

func anyPositive(values []float64) bool {
	for _, v := range values {
		if v > 0 {
			return true
		}
	}
	return false
}

func statusLabel(status string) string {
	switch status {
	case "active":
		return "Active"
	case "achieved":
		return "Achieved"
	case "paused":
		return "Paused"
	case "abandoned":
		return "Abandoned"
	default:
		return status
	}
}

func formatDuration(seconds int) string {
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
