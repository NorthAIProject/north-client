package lifts_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

type bodyweight struct{}

func (bodyweight) Current(context.Context, uuid.UUID) (biometrics.Biometric, error) {
	return biometrics.Biometric{WeightKg: 80, HeightCm: 180, Sex: biometrics.SexMale}, nil
}

func TestCardsForBatchesAcrossPeople(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	reg := users.NewService(users.NewRepository(pool))
	register := func(email string) users.User {
		u, err := reg.Register(ctx, users.Registration{Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "T", Timezone: "UTC"})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	ana, ben := register("ana-card@example.com"), register("ben-card@example.com")

	acts := activity.NewService(activity.NewRepository(pool), bodyweight{})
	svc := lifts.NewService(lifts.NewRepository(pool), nil).WithRecaps(acts, nil)

	now := time.Now().UTC().Truncate(time.Second)
	workout := func(u users.User, start time.Time) activity.Session {
		se, err := acts.Log(ctx, u.ID, activity.LogInput{ActivityCode: "strength_training_light", StartedAt: start, Duration: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		return se
	}
	lift := func(u users.User, se activity.Session, name string, kg float64, reps int) {
		at := se.StartedAt.Add(10 * time.Minute)
		if _, err := svc.Log(ctx, u, lifts.LogInput{ExerciseName: name, SetNumber: 1, WeightKg: kg, Reps: reps, PerformedAt: &at, ActivitySessionID: &se.ID}); err != nil {
			t.Fatal(err)
		}
	}

	anaBefore := workout(ana, now.Add(-10*24*time.Hour))
	lift(ana, anaBefore, "Squat", 100, 5)
	anaNow := workout(ana, now.Add(-3*time.Hour))
	lift(ana, anaNow, "Squat", 105, 5)
	benOnly := workout(ben, now.Add(-2*time.Hour))
	lift(ben, benOnly, "Squat", 90, 5) // Ben's first squat: a baseline, though Ana's was heavier

	cancelled, err := acts.Start(ctx, ana.ID, "strength_training_light", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = acts.Cancel(ctx, cancelled.ID, ana.ID); err != nil {
		t.Fatal(err)
	}

	cards, err := svc.CardsFor(ctx, []uuid.UUID{anaBefore.ID, anaNow.ID, benOnly.ID, cancelled.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 3 {
		t.Fatalf("got %d cards, want the 3 completed workouts", len(cards))
	}
	if _, ok := cards[cancelled.ID]; ok {
		t.Fatal("a cancelled session has no card")
	}
	if c := cards[anaBefore.ID]; c.Sets != 1 || c.PRs != 0 || c.UserID != ana.ID || c.Name != "Strength training (light)" || c.Duration != time.Hour {
		t.Fatalf("Ana's first card = %+v", c)
	}
	if c := cards[anaNow.ID]; c.Sets != 1 || c.PRs != 1 || !c.Exercises[0].PR || c.Exercises[0].Best != "105 kg × 5" {
		t.Fatalf("Ana's second card should be a PR against her own earlier set: %+v", c)
	}
	if c := cards[benOnly.ID]; c.UserID != ben.ID || c.Sets != 1 || c.PRs != 0 || c.Exercises[0].Best != "90 kg × 5" {
		t.Fatalf("Ben's card = %+v", c)
	}

	if none, err := svc.CardsFor(ctx, nil); err != nil || len(none) != 0 {
		t.Fatalf("no ids: %v, %v", none, err)
	}
}

// A regular lifter logs far more than 500 sets a year. The oldest, heaviest
// set must still count against today's, or a lighter session reads as a PR.
func TestCardsForSeesTheWholeYearOfHistory(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	reg := users.NewService(users.NewRepository(pool))
	u, err := reg.Register(ctx, users.Registration{Email: "many-sets@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "T", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	acts := activity.NewService(activity.NewRepository(pool), bodyweight{})
	svc := lifts.NewService(lifts.NewRepository(pool), nil).WithRecaps(acts, nil)

	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 520; i++ {
		at := now.Add(-time.Duration(300*24-i) * time.Hour) // oldest first, all inside the year
		kg := 60.0
		if i == 0 {
			kg = 120 // the oldest set is the heaviest
		}
		if _, err = svc.Log(ctx, u, lifts.LogInput{ExerciseName: "Squat", SetNumber: i%50 + 1, WeightKg: kg, Reps: 5, PerformedAt: &at}); err != nil {
			t.Fatal(err)
		}
	}
	se, err := acts.Log(ctx, u.ID, activity.LogInput{ActivityCode: "strength_training_light", StartedAt: now.Add(-2 * time.Hour), Duration: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	at := se.StartedAt.Add(10 * time.Minute)
	if _, err = svc.Log(ctx, u, lifts.LogInput{ExerciseName: "Squat", SetNumber: 1, WeightKg: 100, Reps: 5, PerformedAt: &at, ActivitySessionID: &se.ID}); err != nil {
		t.Fatal(err)
	}
	cards, err := svc.CardsFor(ctx, []uuid.UUID{se.ID})
	if err != nil {
		t.Fatal(err)
	}
	if c := cards[se.ID]; c.PRs != 0 || c.Exercises[0].PR {
		t.Fatalf("100 kg is lighter than the 120 kg set from months ago, not a PR: %+v", c)
	}
}
