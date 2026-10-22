package workoutcard_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/web/shared/workoutcard"
)

func render(t *testing.T, ctx context.Context, c lift.Card, href string) string {
	t.Helper()
	var b bytes.Buffer
	if err := workoutcard.Card(c, href).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestWorkoutCardOmitsZeroParts(t *testing.T) {
	run := lift.Card{CardSession: lift.CardSession{Name: "Running", Duration: 32 * time.Minute, DistanceM: util.Ptr(5000.0)}, Exercises: []lift.CardExercise{}}
	out := render(t, context.Background(), run, "")
	for _, want := range []string{"5 km", "32 min"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	for _, bad := range []string{"0 sets", "0 kg", "<a "} {
		if strings.Contains(out, bad) {
			t.Errorf("unexpected %q in %s", bad, out)
		}
	}
}

func TestWorkoutCardShowsTopExercisesPRsAndLink(t *testing.T) {
	c := lift.Card{
		CardSession: lift.CardSession{Name: "Weights", Duration: time.Hour, DistanceM: util.Ptr(5200.0)},
		Sets:        9, VolumeKg: 12345, PRs: 1,
		Exercises: []lift.CardExercise{
			{Name: "Squat", Sets: 3, BestWeightKg: 102.5, BestReps: 5, PR: true},
			{Name: "Bench", Sets: 3, BestWeightKg: 80, BestReps: 8},
			{Name: "Row", Sets: 3, BestWeightKg: 60, BestReps: 8},
		},
	}
	out := render(t, i18n.WithLocale(context.Background(), "es"), c, "/app/u/leo/workouts/x")
	for _, want := range []string{"9 series", "12,345 kg", "5,2 km", "102,5 kg × 5", "Squat", "Bench", "PR", `href="/app/u/leo/workouts/x"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "Row") {
		t.Errorf("only the top two exercises: %s", out)
	}
}
