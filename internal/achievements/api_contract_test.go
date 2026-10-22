package achievements

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestFeedShapes(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 7, 30, 0, 0, time.UTC)
	apitest.AssertGolden(t, "feed.golden.json", projectFeed(context.Background(), []Item{{
		ID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), UserID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		DisplayName: "Ana", Handle: "ana_runs", Category: "training", Kind: "workout_completed",
		Title: "Finished Running", Detail: "32 min", OccurredAt: at, Kudos: 3, Kudoed: true,
	}, {
		ID: uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"), UserID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		DisplayName: "Ana", Handle: "ana_runs", Category: "training", Kind: "workout_completed",
		Title: "Finished Weights", Detail: "60 min", OccurredAt: at.Add(-time.Hour),
		Workout: &lift.Card{
			CardSession: lift.CardSession{
				ID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), UserID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
				Name: "Strength training", ActivityCode: "strength_training", StartedAt: at.Add(-2 * time.Hour), EndedAt: at.Add(-time.Hour),
				Duration: time.Hour,
			},
			Sets: 9, VolumeKg: 4200, PRs: 1,
			Exercises: []lift.CardExercise{
				{Name: "Squat", Sets: 3, VolumeKg: 2000, Best: "100 kg × 5", PR: true},
				{Name: "Bench press", Sets: 3, VolumeKg: 1200, Best: "80 kg × 5"},
				{Name: "Row", Sets: 3, VolumeKg: 1000, Best: "70 kg × 5"},
			},
		},
	}}))
	apitest.AssertGolden(t, "sharing.golden.json", SharingView{Training: true, Streaks: false, Goals: true, XP: true, Awards: true})
}
