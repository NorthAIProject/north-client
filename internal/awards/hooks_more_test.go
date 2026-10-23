package awards_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/meals"
)

func areasOf(r *recorder) []award.Area {
	if len(r.areas) == 0 {
		return nil
	}
	return r.areas[0]
}

func TestCheckinHook(t *testing.T) {
	e := newEnv(t, "UTC")
	u, rec := person(t, e), &recorder{}
	svc := checkins.NewService(checkins.NewRepository(e.pool), nil).WithAwards(rec)
	if _, err := svc.UpsertToday(context.Background(), u, checkins.Input{Mood: 0, Energy: 3}); err == nil {
		t.Fatal("invalid check-in accepted")
	}
	rec.none(t)
	if _, err := svc.UpsertToday(context.Background(), u, checkins.Input{Mood: 4, Energy: 3}); err != nil {
		t.Fatal(err)
	}
	rec.only(t, u.ID, award.Consistency)
}

func TestGoalHooks(t *testing.T) {
	e := newEnv(t, "UTC")
	ctx := context.Background()
	rec := &recorder{}
	svc := goals.NewService(goals.NewRepository(e.pool)).WithAwards(rec)
	g, err := svc.Create(ctx, e.user, goals.Input{Title: "Run", Category: "fitness"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetStatus(ctx, g.ID, e.user, "bogus"); err == nil {
		t.Fatal("unknown status accepted")
	}
	if _, err = svc.SetStatus(ctx, g.ID, e.user, goals.StatusPaused); err != nil {
		t.Fatal(err)
	}
	rec.none(t)
	if _, err = svc.SetStatus(ctx, g.ID, e.user, goals.StatusAchieved); err != nil {
		t.Fatal(err)
	}
	rec.only(t, e.user, award.Goals)

	rec2 := &recorder{}
	svc.WithAwards(rec2)
	g2, _ := svc.Create(ctx, e.user, goals.Input{Title: "Swim", Category: "fitness"})
	m, err := svc.AddMilestone(ctx, g2.ID, e.user, goals.MilestoneInput{Title: "First lap"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetMilestoneStatus(ctx, m.ID, e.user, "bogus"); err == nil {
		t.Fatal("unknown milestone status accepted")
	}
	rec2.none(t)
	if _, err = svc.SetMilestoneStatus(ctx, m.ID, e.user, goals.MilestoneCompleted); err != nil {
		t.Fatal(err)
	}
	rec2.only(t, e.user, award.Goals)
}

func TestMealHook(t *testing.T) {
	e := newEnv(t, "UTC")
	ctx := context.Background()
	rec := &recorder{}
	repo := meals.NewRepository(e.pool)
	ing, err := meals.NewIngredientService(repo).Create(ctx, e.user, meals.IngredientInput{
		Name: "Oats", Category: meals.CategoryCarb, Per100g: meals.Macros{Calories: 389, ProteinG: 16.9, FatG: 6.9, CarbG: 66.3},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := meals.NewFoodLogService(repo).WithAwards(rec)
	if _, err = svc.LogIngredient(ctx, e.user, meals.LogIngredientInput{IngredientID: ing.ID, QuantityGrams: 0}); err == nil {
		t.Fatal("zero quantity accepted")
	}
	rec.none(t)
	if _, err = svc.LogIngredient(ctx, e.user, meals.LogIngredientInput{IngredientID: ing.ID, QuantityGrams: 50}); err != nil {
		t.Fatal(err)
	}
	rec.only(t, e.user, award.Nutrition)
}

func TestLiftHook(t *testing.T) {
	e := newEnv(t, "UTC")
	u, rec := person(t, e), &recorder{}
	svc := lifts.NewService(lifts.NewRepository(e.pool), exercises.NewService(exercises.NewRepository(e.pool))).WithAwards(rec)
	if _, err := svc.Log(context.Background(), u, lifts.LogInput{ExerciseName: "Squat", SetNumber: 1, WeightKg: 100, Reps: 0}); err == nil {
		t.Fatal("zero reps accepted")
	}
	rec.none(t)
	if _, err := svc.Log(context.Background(), u, lifts.LogInput{ExerciseName: "Squat", SetNumber: 1, WeightKg: 100, Reps: 5}); err != nil {
		t.Fatal(err)
	}
	rec.only(t, u.ID, award.Lifts)
}

func TestHealthIngestHook(t *testing.T) {
	e := newEnv(t, "UTC")
	ctx := context.Background()
	rec := &recorder{}
	svc := health.NewService(health.NewRepository(e.pool)).WithAwards(rec)
	hr := []health.Reading{{Metric: "heart_rate", Value: 60, Unit: "bpm", StartedAt: now}}
	if _, err := svc.Ingest(ctx, e.user, health.SourceAppleHealth, []health.Reading{{Metric: "", Value: 1}}); err == nil {
		t.Fatal("invalid reading accepted")
	}
	if _, err := svc.Ingest(ctx, e.user, health.SourceManual, hr); err != nil {
		t.Fatal(err)
	}
	rec.none(t)
	if _, err := svc.Ingest(ctx, e.user, health.SourceAppleHealth, hr); err != nil {
		t.Fatal(err)
	}
	rec.only(t, e.user, award.Body)
}

func importer(t *testing.T, e *env, aw activity.Awards) *activity.Service {
	t.Helper()
	svc := activity.NewService(activity.NewRepository(e.pool), biometrics.NewService(biometrics.NewRepository(e.pool)))
	if aw != nil {
		svc.WithAwards(aw)
	}
	return svc
}

func importOne(svc *activity.Service, user uuid.UUID, ext, source string, ended time.Time, skip bool) (bool, error) {
	_, created, err := svc.Import(context.Background(), activity.ImportInput{
		UserID: user, ActivityCode: "running_8kmh", Source: source, ExternalID: ext,
		StartedAt: ended.Add(-30 * time.Minute), EndedAt: ended, WeightKg: 70, SkipAwards: skip,
	})
	return created, err
}

func TestActivityImportHook(t *testing.T) {
	e := newEnv(t, "UTC")
	rec := &recorder{}
	svc := importer(t, e, rec)
	old := time.Now().AddDate(-2, 0, 0)

	if _, _, err := svc.Import(context.Background(), activity.ImportInput{
		UserID: e.user, ActivityCode: "nope", Source: activity.SourceStrava, ExternalID: "bad",
		StartedAt: old.Add(-time.Hour), EndedAt: old, WeightKg: 70,
	}); err == nil {
		t.Fatal("unknown activity accepted")
	}
	rec.none(t)
	if _, err := importOne(svc, e.user, "a1", activity.SourceStrava, old, false); err != nil {
		t.Fatal(err)
	}
	if len(rec.users) != 1 {
		t.Fatalf("%d checks, want 1", len(rec.users))
	}
	if a := areasOf(rec); len(a) != 2 || a[0] != award.Training || a[1] != award.Body {
		t.Fatalf("strava import areas = %v, want training and body", a)
	}
	// The same activity again is not new: no second check.
	if _, err := importOne(svc, e.user, "a1", activity.SourceStrava, old, false); err != nil {
		t.Fatal(err)
	}
	if len(rec.users) != 1 {
		t.Fatalf("a duplicate import checked again: %d checks", len(rec.users))
	}
}

func TestImportedBadgeIsStampedNow(t *testing.T) {
	e := newEnv(t, "UTC")
	svc := importer(t, e, e.svc)
	if _, err := importOne(svc, e.user, "old1", activity.SourceStrava, time.Now().AddDate(-2, 0, 0), false); err != nil {
		t.Fatal(err)
	}
	var at time.Time
	if err := e.pool.QueryRow(context.Background(),
		`SELECT occurred_at FROM achievements WHERE user_id=$1 AND kind='badge_earned' AND source_key='workouts_1'`, e.user).Scan(&at); err != nil {
		t.Fatalf("workouts_1 not earned: %v", err)
	}
	if d := time.Since(at); d < 0 || d > time.Minute {
		t.Fatalf("badge stamped %v ago, want about now", d)
	}
}

func TestBatchImportChecksOnce(t *testing.T) {
	e := newEnv(t, "UTC")
	ctx := context.Background()
	rec := &recorder{}
	act := importer(t, e, rec)
	bio := biometrics.NewService(biometrics.NewRepository(e.pool))
	if _, err := bio.Record(ctx, e.user, biometrics.Input{WeightKg: 70, HeightCm: 175, DateOfBirth: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), Sex: "male"}); err != nil {
		t.Fatal(err)
	}
	hs := health.NewService(health.NewRepository(e.pool)).WithAwards(rec).WithWorkouts(act, bio)

	var ws []health.Workout
	for i := 0; i < 5; i++ {
		end := now.Add(-time.Duration(i) * 24 * time.Hour)
		ws = append(ws, health.Workout{ActivityCode: "running_8kmh", ExternalID: string(rune('a' + i)), StartedAt: end.Add(-time.Hour), EndedAt: end})
	}
	if _, err := hs.IngestWorkouts(ctx, e.user, health.SourceAppleHealth, ws); err != nil {
		t.Fatal(err)
	}
	rec.only(t, e.user, award.Training)
	// Replaying the payload adds nothing new, so nothing is checked.
	if _, err := hs.IngestWorkouts(ctx, e.user, health.SourceAppleHealth, ws); err != nil {
		t.Fatal(err)
	}
	if len(rec.users) != 1 {
		t.Fatalf("%d checks for two deliveries of 5 workouts, want 1", len(rec.users))
	}
}

func TestBatchImportChecksWhatItImportedBeforeAFailure(t *testing.T) {
	e := newEnv(t, "UTC")
	ctx := context.Background()
	rec := &recorder{}
	act := importer(t, e, rec)
	bio := biometrics.NewService(biometrics.NewRepository(e.pool))
	if _, err := bio.Record(ctx, e.user, biometrics.Input{WeightKg: 70, HeightCm: 175, DateOfBirth: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), Sex: "male"}); err != nil {
		t.Fatal(err)
	}
	hs := health.NewService(health.NewRepository(e.pool)).WithAwards(rec).WithWorkouts(act, bio)
	mk := func(id, code string, daysAgo int) health.Workout {
		end := now.Add(-time.Duration(daysAgo) * 24 * time.Hour)
		return health.Workout{ActivityCode: code, ExternalID: id, StartedAt: end.Add(-time.Hour), EndedAt: end}
	}
	ws := []health.Workout{mk("a", "running_8kmh", 1), mk("b", "nope", 2), mk("c", "running_8kmh", 3)}
	if _, err := hs.IngestWorkouts(ctx, e.user, health.SourceAppleHealth, ws); err == nil {
		t.Fatal("a failing workout did not fail the batch")
	}
	rec.only(t, e.user, award.Training)
}
