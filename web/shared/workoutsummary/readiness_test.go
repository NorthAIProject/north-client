package workoutsummary_test

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/web/shared/workoutsummary"
)

func TestNewReadinessSortsMusclesIntoTiers(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	sets := func(slug string, n int, ago time.Duration) []lift.Set {
		out := make([]lift.Set, n)
		for i := range out {
			out[i] = lift.Set{ExerciseSlug: slug, PerformedAt: now.Add(-ago), WeightKg: 60, Reps: 8}
		}
		return out
	}
	weights := lift.Weights{
		"bench": lift.MuscleWeights([]string{"chest"}, []string{"triceps"}),
		"row":   lift.MuscleWeights([]string{"lats"}, nil),
		"squat": lift.MuscleWeights([]string{"quads"}, nil),
	}
	all := append(sets("bench", 8, 2*time.Hour), sets("row", 3, 5*day)...)
	all = append(all, sets("squat", 4, 30*day)...)

	r := workoutsummary.NewReadiness(lift.LoadOf(all, weights, now))
	if strings.Join(r.Fatigued, ",") != "chest" || strings.Join(r.Recovering, ",") != "triceps" {
		t.Errorf("fatigued %v, recovering %v", r.Fatigued, r.Recovering)
	}
	if strings.Join(r.Fresh, ",") != "lats" {
		t.Errorf("fresh = %v, want lats (trained five days ago, recovered)", r.Fresh)
	}
	if strings.Join(r.Stale, ",") != "Quads · 30 days" {
		t.Errorf("stale = %v", r.Stale)
	}
}
