package lifts_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts"
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
