package insights

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// liftingExercises caps the exercise table: the long tail is one-off
// accessories, and the page is about the main lifts.
const liftingExercises = 8

func buildLiftingView(ctx context.Context, st lifts.Stats) insightpages.LiftingView {
	if len(st.Sets) == 0 {
		return insightpages.LiftingView{}
	}
	view := insightpages.LiftingView{
		HasSets:  true,
		Volume:   formatKg(ctx, st.VolumeKg()),
		Delta:    deltaView(st.VolumeKg(), st.PriorVolumeKg),
		Sets:     len(st.Sets),
		Workouts: workoutCount(st),
	}

	labels := make([]string, len(st.Weekly))
	totals := make([]float64, len(st.Weekly))
	for i, w := range st.Weekly {
		labels[i] = i18n.Date(ctx, w.Day, "Jan 2")
		totals[i] = w.Value
	}
	view.HasWeekly = len(st.Weekly) > 1
	view.WeeklyChart = viz.Bar("insights-lifting-weekly", "kg", labels, totals)

	for i, e := range st.Exercises {
		if i == liftingExercises {
			break
		}
		row := insightpages.LiftExerciseRow{
			Name: e.Name, Sets: e.Sets, Best: formatKg(ctx, e.BestWeightKg), E1RM: formatKg(ctx, e.BestE1RM), Volume: formatKg(ctx, e.VolumeKg),
		}
		if n := len(e.Trend); n > 1 {
			row.Change = util.RoundHalfUpToScale(e.Trend[n-1].Value-e.Trend[0].Value, 1)
		}
		view.Exercises = append(view.Exercises, row)
	}

	for _, r := range st.Records {
		view.Records = append(view.Records, insightpages.LiftRecordRow{
			Exercise: r.Set.ExerciseName,
			Lift:     fmt.Sprintf("%s × %d", formatKg(ctx, r.Set.WeightKg), r.Set.Reps),
			E1RM:     formatKg(ctx, r.E1RM),
			Gain:     formatKg(ctx, util.RoundHalfUpToScale(r.E1RM-r.Previous, 1)),
			On:       i18n.Date(ctx, r.Set.LogDate, "Mon, Jan 2"),
		})
	}

	most := 0
	for _, m := range st.Muscles {
		most = max(most, m.Sets)
	}
	for _, m := range st.Muscles {
		view.Muscles = append(view.Muscles, insightpages.MuscleRow{
			Name: muscleLabel(ctx, m.Muscle), Sets: m.Sets, Pct: m.Sets * 100 / most,
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

func formatKg(ctx context.Context, v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%.0f kg", v)
	}
	return i18n.Decimal(ctx, v, 1) + " kg"
}

// muscleLabel turns a muscle key ("lower_back") into words ("Lower back"):
// the body figure's name for it when the catalogue has one, so the page and
// the figure agree, and the key spelled out otherwise.
func muscleLabel(ctx context.Context, key string) string {
	if name := i18n.T(ctx, "muscle."+key); name != "muscle."+key {
		return name
	}
	words := strings.ReplaceAll(key, "_", " ")
	if words == "" {
		return ""
	}
	return strings.ToUpper(words[:1]) + words[1:]
}
