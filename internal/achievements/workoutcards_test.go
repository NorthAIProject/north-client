package achievements_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/social"
)

type bodyweight struct{}

func (bodyweight) Current(context.Context, uuid.UUID) (biometrics.Biometric, error) {
	return biometrics.Biometric{WeightKg: 80, HeightCm: 180, Sex: biometrics.SexMale}, nil
}

func TestFeedAttachesWorkoutCards(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	acts := activity.NewService(activity.NewRepository(pool), bodyweight{})
	liftSvc := lifts.NewService(lifts.NewRepository(pool), nil).WithRecaps(acts, nil)
	ach := achievements.NewService(achievements.NewRepository(pool)).WithWorkoutCards(liftSvc)
	soc := social.NewService(social.NewRepository(pool))
	ana := person(t, pool, "ana-wc@north.test", "Ana")
	leo := person(t, pool, "leo-wc@north.test", "Leo")
	follow(t, soc, ana, leo, "leo")

	start := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	se, err := acts.Log(ctx, leo.ID, activity.LogInput{ActivityCode: "strength_training_light", StartedAt: start, Duration: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	at := start.Add(10 * time.Minute)
	for i, kg := range []float64{100, 100} {
		if _, err = liftSvc.Log(ctx, leo, lifts.LogInput{ExerciseName: "Squat", SetNumber: i + 1, WeightKg: kg, Reps: 5, PerformedAt: &at, ActivitySessionID: &se.ID}); err != nil {
			t.Fatal(err)
		}
	}
	// Log records the achievement itself; reporting it again is a no-op.
	ach.WorkoutCompleted(ctx, leo.ID, se.ID, "Strength", 60, start.Add(time.Hour))

	items, err := ach.Feed(ctx, ana.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SourceKey != se.ID.String() {
		t.Fatalf("items = %+v", items)
	}
	w := items[0].Workout
	if w == nil || w.Sets != 2 || w.VolumeKg != 1000 {
		t.Fatalf("workout = %+v", w)
	}
}

var errBoom = errors.New("boom")

type failingCards struct{}

func (failingCards) CardsFor(context.Context, []uuid.UUID) (map[uuid.UUID]lift.Card, error) {
	return nil, errBoom
}

func TestFeedSurvivesABrokenWorkoutReference(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	acts := activity.NewService(activity.NewRepository(pool), bodyweight{})
	liftSvc := lifts.NewService(lifts.NewRepository(pool), nil).WithRecaps(acts, nil)
	ach := achievements.NewService(achievements.NewRepository(pool)).WithWorkoutCards(liftSvc)
	soc := social.NewService(social.NewRepository(pool))
	ana := person(t, pool, "ana-bk@north.test", "Ana")
	leo := person(t, pool, "leo-bk@north.test", "Leo")
	follow(t, soc, ana, leo, "leo2")

	if _, err := pool.Exec(ctx, `INSERT INTO achievements (user_id, category, kind, title, source_key, occurred_at)
		VALUES ($1, 'training', 'workout_completed', 'Finished odd', 'not-a-uuid', now() - interval '1 hour')`, leo.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := acts.Start(ctx, leo.ID, "strength_training_light", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = acts.Cancel(ctx, cancelled.ID, leo.ID); err != nil {
		t.Fatal(err)
	}
	ach.WorkoutCompleted(ctx, leo.ID, cancelled.ID, "Cancelled", 5, time.Now().Add(-2*time.Hour))

	items, err := ach.Feed(ctx, ana.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	for _, it := range items {
		if it.Workout != nil {
			t.Fatalf("%s should have no card: %+v", it.Title, it.Workout)
		}
	}

	// A card lookup that errors costs the cards, never the feed.
	broken := achievements.NewService(achievements.NewRepository(pool)).WithWorkoutCards(failingCards{})
	items, err = broken.Feed(ctx, ana.ID, time.Time{})
	if err != nil || len(items) != 2 {
		t.Fatalf("feed with failing cards: %v, %d items", err, len(items))
	}
}
