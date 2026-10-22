package lift_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

func cardSession(d time.Duration) lift.CardSession {
	start := time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	return lift.CardSession{ID: uuid.New(), UserID: uuid.New(), Name: "Weight training", ActivityCode: "weight_training", StartedAt: start, EndedAt: start.Add(d), Duration: d}
}

func cardSet(name, kind string, kg float64, reps int, at time.Time) lift.Set {
	return lift.Set{ID: uuid.New(), ExerciseName: name, WeightKg: kg, Reps: reps, PerformedAt: at, Kind: kind}
}

func TestCardCountsWorkingSetsAndOrdersByVolume(t *testing.T) {
	s := cardSession(45 * time.Minute)
	at := s.StartedAt
	current := []lift.Set{
		cardSet("Bench press", lift.KindWork, 60, 8, at),
		cardSet("Bench press", lift.KindWork, 60, 8, at.Add(time.Minute)),
		cardSet("Bench press", lift.KindWork, 60, 8, at.Add(2*time.Minute)),
		cardSet("Squat", lift.KindWarmup, 60, 5, at.Add(3*time.Minute)),
		cardSet("Squat", lift.KindWork, 100, 5, at.Add(4*time.Minute)),
		cardSet("Squat", lift.KindWork, 100, 5, at.Add(5*time.Minute)),
		cardSet("Squat", lift.KindWork, 100, 5, at.Add(6*time.Minute)),
	}
	c := lift.BuildCard(s, current, nil)
	if c.Sets != 6 || c.VolumeKg != 2940 {
		t.Fatalf("sets %d volume %v, want 6 and 2940", c.Sets, c.VolumeKg)
	}
	if len(c.Exercises) != 2 || c.Exercises[0].Name != "Squat" || c.Exercises[0].Best != "100 kg × 5" || c.Exercises[0].Sets != 3 {
		t.Fatalf("exercises = %+v", c.Exercises)
	}
	if c.PRs != 0 {
		t.Fatalf("PRs = %d, want 0: with no history the first time is a baseline", c.PRs)
	}
	if c.Duration != 45*time.Minute {
		t.Fatalf("duration %v", c.Duration)
	}
}

func TestCardFlagsAPROnlyAgainstEarlierSets(t *testing.T) {
	s := cardSession(time.Hour)
	current := []lift.Set{cardSet("Squat", lift.KindWork, 105, 5, s.StartedAt)}
	before := s.StartedAt.Add(-72 * time.Hour)

	c := lift.BuildCard(s, current, []lift.Set{cardSet("Squat", lift.KindWork, 100, 5, before)})
	if !c.Exercises[0].PR || c.PRs != 1 {
		t.Fatalf("heavier than before: PR %v, PRs %d", c.Exercises[0].PR, c.PRs)
	}

	c = lift.BuildCard(s, current, []lift.Set{cardSet("Squat", lift.KindWork, 110, 5, before)})
	if c.Exercises[0].PR || c.PRs != 0 {
		t.Fatalf("lighter than before: PR %v, PRs %d", c.Exercises[0].PR, c.PRs)
	}
}

func TestCardForARunWithNoSets(t *testing.T) {
	s := cardSession(30 * time.Minute)
	dist := 5000.0
	s.DistanceM = &dist
	c := lift.BuildCard(s, nil, nil)
	if c.Sets != 0 || c.VolumeKg != 0 || len(c.Exercises) != 0 || c.Duration != 30*time.Minute || *c.DistanceM != 5000 {
		t.Fatalf("card = %+v", c)
	}
}

func TestCardBestInUsesTheReadersDecimalMark(t *testing.T) {
	e := lift.CardExercise{Best: "22.5 kg × 8", BestWeightKg: 22.5, BestReps: 8}
	if got := e.BestIn("es"); got != "22,5 kg × 8" {
		t.Fatalf("BestIn(es) = %q", got)
	}
	if got := e.BestIn("en"); got != "22.5 kg × 8" {
		t.Fatalf("BestIn(en) = %q", got)
	}
}

func TestCardBodyweightExerciseHasNoBest(t *testing.T) {
	s := cardSession(20 * time.Minute)
	c := lift.BuildCard(s, []lift.Set{cardSet("Pull-up", lift.KindWork, 0, 10, s.StartedAt)}, nil)
	if e := c.Exercises[0]; e.Best != "" || e.BestIn("es") != "" || e.Sets != 1 {
		t.Fatalf("exercise = %+v", e)
	}
}
