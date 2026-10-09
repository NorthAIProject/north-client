package lifts_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestLogLastAndStats(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	reg := users.NewService(users.NewRepository(pool))
	user, err := reg.Register(ctx, users.Registration{
		Email: "lift@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "T", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := lifts.NewService(lifts.NewRepository(pool), nil)

	earlier := time.Now().Add(-48 * time.Hour)
	for i, kg := range []float64{100, 100} {
		if _, err = svc.Log(ctx, user, lifts.LogInput{ExerciseName: "Squat", SetNumber: i + 1, WeightKg: kg, Reps: 5, PerformedAt: &earlier}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := svc.Log(ctx, user, lifts.LogInput{ExerciseName: " squat", SetNumber: 1, WeightKg: 107.5, Reps: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Log(ctx, user, lifts.LogInput{ExerciseName: "Squat", SetNumber: 1, WeightKg: 900, Reps: 5}); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("900 kg = %v, want a validation error", err)
	}

	last, err := svc.Last(ctx, user, []string{"Squat", "Deadlift"})
	if err != nil {
		t.Fatal(err)
	}
	if got := last["squat"]; len(got) != 1 || got[0].WeightKg != 107.5 {
		t.Errorf("last squat = %+v", got)
	}
	if _, ok := last["deadlift"]; ok {
		t.Error("an exercise never done has a last workout")
	}

	st, err := svc.Stats(ctx, user, timerange.Parse(timerange.KeyWeek, user.Location()))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Records) != 1 || st.Records[0].Set.ID != first.ID {
		t.Errorf("records = %+v", st.Records)
	}

	// Another person's sets never show up.
	other, err := reg.Register(ctx, users.Registration{
		Email: "other@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "O", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Undo(ctx, other, first.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("undo someone else's set = %v", err)
	}
	if err = svc.Undo(ctx, user, first.ID); err != nil {
		t.Fatal(err)
	}
}

// catalog is a one-exercise MuscleLookup.
type catalog map[string]exercises.Exercise

func (c catalog) Resolve(_ context.Context, slugs []string) (map[string]exercises.Exercise, error) {
	out := map[string]exercises.Exercise{}
	for _, s := range slugs {
		if e, ok := c[s]; ok {
			out[s] = e
		}
	}
	return out, nil
}

func TestReadinessReadsMusclesFromTheCatalog(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "ready@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "R", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := lifts.NewService(lifts.NewRepository(pool), catalog{
		"barbell-squat": {Slug: "barbell-squat", Primary: []string{"quads"}, Secondary: []string{"hamstrings"}},
	})

	empty, err := svc.Readiness(ctx, user)
	if err != nil || !empty.LastSession.IsZero() {
		t.Fatalf("no sets: load = %+v, err = %v", empty, err)
	}

	hourAgo := time.Now().Add(-time.Hour)
	for i := range 8 {
		if _, err = svc.Log(ctx, user, lifts.LogInput{ExerciseName: "Squat", ExerciseSlug: "barbell-squat", SetNumber: i + 1, WeightKg: 100, Reps: 5, PerformedAt: &hourAgo}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = svc.Log(ctx, user, lifts.LogInput{ExerciseName: "Mystery machine", SetNumber: 1, WeightKg: 40, Reps: 10, PerformedAt: &hourAgo}); err != nil {
		t.Fatal(err)
	}

	load, err := svc.Readiness(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if fatigued := load.In(lift.StateFatigued); len(fatigued) != 1 || fatigued[0].Muscle != "quads" {
		t.Errorf("fatigued = %+v, want quads", fatigued)
	}
	if recovering := load.In(lift.StateRecovering); len(recovering) != 1 || recovering[0].Muscle != "hamstrings" {
		t.Errorf("recovering = %+v, want hamstrings", recovering)
	}
	if load.Unmapped != 1 {
		t.Errorf("unmapped = %d, want the typed-in set", load.Unmapped)
	}
}

func TestLogKeepsKindAndRIR(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "kinds@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "K", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := lifts.NewService(lifts.NewRepository(pool), nil)

	two := 2
	warm, err := svc.Log(ctx, user, lifts.LogInput{ExerciseName: "Bench", SetNumber: 1, WeightKg: 40, Reps: 10, Kind: lift.KindWarmup})
	if err != nil {
		t.Fatal(err)
	}
	work, err := svc.Log(ctx, user, lifts.LogInput{ExerciseName: "Bench", SetNumber: 2, WeightKg: 80, Reps: 5, RIR: &two})
	if err != nil {
		t.Fatal(err)
	}
	if warm.Kind != lift.KindWarmup || warm.RIR != nil {
		t.Errorf("warm-up = %+v", warm)
	}
	if work.Kind != lift.KindWork || work.RIR == nil || *work.RIR != 2 {
		t.Errorf("work set = %+v, want kind work and rir 2", work)
	}

	eleven := 11
	if _, err = svc.Log(ctx, user, lifts.LogInput{ExerciseName: "Bench", SetNumber: 3, WeightKg: 80, Reps: 5, Kind: "failure"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("unknown kind = %v, want a validation error", err)
	}
	if _, err = svc.Log(ctx, user, lifts.LogInput{ExerciseName: "Bench", SetNumber: 3, WeightKg: 80, Reps: 5, RIR: &eleven}); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("rir 11 = %v, want a validation error", err)
	}

	st, err := svc.Stats(ctx, user, timerange.Parse(timerange.KeyWeek, user.Location()))
	if err != nil {
		t.Fatal(err)
	}
	if st.VolumeKg() != 400 {
		t.Errorf("volume = %v, want 400 (the warm-up does not count)", st.VolumeKg())
	}
}
