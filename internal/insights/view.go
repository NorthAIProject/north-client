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
	buckets := data.Range.Buckets()
	labels := make([]string, len(buckets))
	water := make([]float64, len(buckets))
	sleepHours := make([]float64, len(buckets))

	for i, b := range buckets {
		labels[i] = b.Label
	}

	loc := data.Range.Location()
	byDay := make(map[string]int, len(data.Hydration))
	for _, d := range data.Hydration {
		byDay[d.Date.In(loc).Format("2006-01-02")] = d.TotalML
	}
	for i, b := range buckets {
		water[i] = float64(byDay[b.Start.Format("2006-01-02")])
	}

	nightsByDay := make(map[string]int, len(data.Nights))
	for _, n := range data.Nights {
		nightsByDay[n.LocalDate.In(loc).Format("2006-01-02")] = n.DurationMinutes
	}
	for i, b := range buckets {
		sleepHours[i] = float64(nightsByDay[b.Start.Format("2006-01-02")]) / 60
	}

	view := insightpages.BodyView{
		Range:        rangeView(data.Range),
		WaterChart:   viz.Bar("insights-body-water", "Water (ml)", labels, water),
		SleepChart:   viz.SingleLine("insights-body-sleep", "Hours slept", labels, sleepHours, nil, nil),
		TotalWaterML: sumInts(water),
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
	buckets := data.Range.Buckets()
	labels := make([]string, len(buckets))
	mood := make([]int, len(buckets))
	energy := make([]int, len(buckets))
	journalCount := make([]float64, len(buckets))
	cells := make([]viz.HeatmapCell, len(buckets))

	loc := data.Range.Location()
	byDay := make(map[string]int, len(buckets))
	for i, b := range buckets {
		labels[i] = b.Label
		byDay[b.Start.Format("2006-01-02")] = i
	}

	for _, c := range data.CheckIns {
		if i, ok := byDay[c.LocalDate.In(loc).Format("2006-01-02")]; ok {
			mood[i] = c.Mood
			energy[i] = c.Energy
		}
	}
	for _, e := range data.Journal {
		if i, ok := byDay[e.CreatedAt.In(loc).Format("2006-01-02")]; ok {
			journalCount[i]++
		}
	}
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
	buckets := data.Range.Buckets()
	labels := make([]string, len(buckets))
	notes := make([]float64, len(buckets))

	loc := data.Range.Location()
	byDay := make(map[string]int, len(buckets))
	for i, b := range buckets {
		labels[i] = b.Label
		byDay[b.Start.Format("2006-01-02")] = i
	}
	for _, n := range data.Notes {
		if i, ok := byDay[n.CreatedAt.In(loc).Format("2006-01-02")]; ok {
			notes[i]++
		}
	}

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
	buckets := data.Range.Buckets()
	labels := make([]string, len(buckets))
	burn := make([]float64, len(buckets))

	loc := data.Range.Location()
	byDay := make(map[string]int, len(buckets))
	for i, b := range buckets {
		labels[i] = b.Label
		byDay[b.Start.Format("2006-01-02")] = i
	}

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
		if i, ok := byDay[ended.Format("2006-01-02")]; ok && sess.CaloriesBurned != nil {
			burn[i] += *sess.CaloriesBurned
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

func sumInts(values []float64) int {
	total := 0.0
	for _, v := range values {
		total += v
	}
	return int(total)
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
