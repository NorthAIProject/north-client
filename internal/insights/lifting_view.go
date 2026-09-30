package insights

import (
	"fmt"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// liftingExercises caps the exercise table: the long tail is one-off
// accessories, and the page is about the main lifts.
const liftingExercises = 8

func buildLiftingView(st lifts.Stats) insightpages.LiftingView {
	if len(st.Sets) == 0 {
		return insightpages.LiftingView{}
	}
	view := insightpages.LiftingView{
		HasSets:  true,
		Volume:   formatKg(st.VolumeKg()),
		Delta:    deltaView(st.VolumeKg(), st.PriorVolumeKg),
		Sets:     len(st.Sets),
		Workouts: workoutCount(st),
	}

	labels := make([]string, len(st.Weekly))
	totals := make([]float64, len(st.Weekly))
	for i, w := range st.Weekly {
		labels[i] = w.Day.Format("Jan 2")
		totals[i] = w.Value
	}
	view.HasWeekly = len(st.Weekly) > 1
	view.WeeklyChart = viz.Bar("insights-lifting-weekly", "kg", labels, totals)

	for i, e := range st.Exercises {
		if i == liftingExercises {
			break
		}
		row := insightpages.LiftExerciseRow{
			Name: e.Name, Sets: e.Sets, Best: formatKg(e.BestWeightKg), E1RM: formatKg(e.BestE1RM), Volume: formatKg(e.VolumeKg),
		}
		if n := len(e.Trend); n > 1 {
			row.Change = util.RoundHalfUpToScale(e.Trend[n-1].Value-e.Trend[0].Value, 1)
		}
		view.Exercises = append(view.Exercises, row)
	}

	for _, r := range st.Records {
		view.Records = append(view.Records, insightpages.LiftRecordRow{
			Exercise: r.Set.ExerciseName,
			Lift:     fmt.Sprintf("%s × %d", formatKg(r.Set.WeightKg), r.Set.Reps),
			E1RM:     formatKg(r.E1RM),
			Gain:     formatKg(util.RoundHalfUpToScale(r.E1RM-r.Previous, 1)),
			On:       r.Set.LogDate.Format("Mon, Jan 2"),
		})
	}

	most := 0
	for _, m := range st.Muscles {
		most = max(most, m.Sets)
	}
	for _, m := range st.Muscles {
		view.Muscles = append(view.Muscles, insightpages.MuscleRow{
			Name: muscleLabel(m.Muscle), Sets: m.Sets, Pct: m.Sets * 100 / most,
		})
	}
	return view
}

func workoutCount(st lifts.Stats) int {
	seen := map[string]bool{}
	for _, s := range st.Sets {
		key := s.LogDate.Format(time.DateOnly)
		if s.ActivitySessionID != nil {
			key = s.ActivitySessionID.String()
		}
		seen[key] = true
	}
	return len(seen)
}

func formatKg(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%.0f kg", v)
	}
	return fmt.Sprintf("%.1f kg", v)
}

// muscleLabel turns a muscle key ("lower_back") into words ("Lower back").
func muscleLabel(key string) string {
	words := strings.ReplaceAll(key, "_", " ")
	if words == "" {
		return ""
	}
	return strings.ToUpper(words[:1]) + words[1:]
}
