package achievements_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
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

// The feed's cards cross people: a page holds the viewer's own workouts and
// those of everyone they may see. Each visible workout keeps its card, down to
// its sets, and nobody else's reaches the viewer.
func TestFeedCardsFollowWhoTheViewerMaySee(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	acts := activity.NewService(activity.NewRepository(pool), bodyweight{})
	liftSvc := lifts.NewService(lifts.NewRepository(pool), nil).WithRecaps(acts, nil)
	ach := achievements.NewService(achievements.NewRepository(pool)).WithWorkoutCards(liftSvc)
	soc := social.NewService(social.NewRepository(pool))

	viewer := person(t, pool, "viewer-fv@north.test", "Viewer")
	friend := person(t, pool, "friend-fv@north.test", "Friend")
	unshared := person(t, pool, "unshared-fv@north.test", "Unshared")
	blocked := person(t, pool, "blocked-fv@north.test", "Blocked")
	stranger := person(t, pool, "stranger-fv@north.test", "Stranger")
	follow(t, soc, viewer, friend, "friend")
	follow(t, soc, viewer, unshared, "unshared")
	follow(t, soc, viewer, blocked, "blocked")
	if _, err := ach.SetSharing(ctx, unshared.ID, achievements.Sharing{Training: false}); err != nil {
		t.Fatal(err)
	}
	if err := soc.Block(ctx, blocked.ID, viewer.ID); err != nil {
		t.Fatal(err)
	}

	// Every person's set count is their own, so a card built from anybody
	// else's rows shows up as the wrong number.
	sets := map[uuid.UUID]int{viewer.ID: 1, friend.ID: 2, unshared.ID: 3, blocked.ID: 4, stranger.ID: 5}
	start := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	sessions := map[uuid.UUID]uuid.UUID{}
	for _, u := range []users.User{viewer, friend, unshared, blocked, stranger} {
		se, err := acts.Log(ctx, u.ID, activity.LogInput{ActivityCode: "strength_training_light", StartedAt: start, Duration: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		sessions[u.ID] = se.ID
		at := start.Add(10 * time.Minute)
		for i := range sets[u.ID] {
			if _, err = liftSvc.Log(ctx, u, lifts.LogInput{ExerciseName: "Squat", SetNumber: i + 1, WeightKg: 100, Reps: 5, PerformedAt: &at, ActivitySessionID: &se.ID}); err != nil {
				t.Fatal(err)
			}
		}
		ach.WorkoutCompleted(ctx, u.ID, se.ID, "Strength", 60, start.Add(time.Hour))
	}

	items, err := ach.Feed(ctx, viewer.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]*lift.Card{}
	for _, it := range items {
		if it.Kind == achievement.KindWorkoutCompleted {
			got[it.UserID] = it.Workout
		}
	}
	if len(got) != 2 {
		t.Fatalf("workout items from %d people, want the viewer and the friend: %+v", len(got), got)
	}
	for _, u := range []users.User{viewer, friend} {
		c := got[u.ID]
		if c == nil || c.ID != sessions[u.ID] || c.UserID != u.ID || c.Sets != sets[u.ID] {
			t.Fatalf("card of %s = %+v, want session %v with %d sets", u.DisplayName, c, sessions[u.ID], sets[u.ID])
		}
	}
}

var errBoom = errors.New("boom")

type failingCards struct{}

func (failingCards) CardsFor(context.Context, []uuid.UUID, []uuid.UUID) (map[uuid.UUID]lift.Card, error) {
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
