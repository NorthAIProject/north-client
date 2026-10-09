package lifts_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestImportHevyWritesSessionsOnceAndJoinsAppleHealth(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "hevy@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "H", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}
	activities := activity.NewService(activity.NewRepository(pool), nil)
	catalog := exercises.NewService(exercises.NewRepository(pool))
	svc := lifts.NewService(lifts.NewRepository(pool), catalog).WithImports(activities, nil, catalog)

	// Apple Health already has leg day: Hevy wrote it there.
	legStart := time.Date(2026, 10, 6, 7, 30, 0, 0, user.Location())
	health, created, err := activities.Import(ctx, activity.ImportInput{
		UserID: user.ID, ActivityCode: "strength_training", Source: "apple_health", ExternalID: "hk-leg-day",
		StartedAt: legStart, EndedAt: legStart.Add(50 * time.Minute),
	})
	if err != nil || !created {
		t.Fatalf("seed apple health session: %v, created %v", err, created)
	}

	importFile := func() lifts.ImportResult {
		t.Helper()
		f, openErr := os.Open("hevy/testdata/workouts.csv")
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer func() { _ = f.Close() }()
		res, importErr := svc.ImportHevy(ctx, user, f)
		if importErr != nil {
			t.Fatal(importErr)
		}
		return res
	}

	first := importFile()
	if first.Workouts != 2 || first.Sets != 6 || first.Skipped != 2 || first.Duplicates != 0 || len(first.Unmatched) != 0 {
		t.Errorf("first import = %+v", first)
	}
	again := importFile()
	if again.Workouts != 0 || again.Sets != 0 || again.Duplicates != 2 {
		t.Errorf("second import = %+v, want both workouts as duplicates", again)
	}

	legSets, err := svc.Between(ctx, user, timerange.Between(legStart, legStart.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if len(legSets) != 1 || legSets[0].ActivitySessionID == nil || *legSets[0].ActivitySessionID != health.ID {
		t.Errorf("leg day sets = %+v, want one set on the Apple Health session", legSets)
	}
	if legSets[0].ExerciseSlug != "barbell-full-squat" || legSets[0].RIR == nil || *legSets[0].RIR != 1 {
		t.Errorf("squat = %+v, want the catalog squat with 1 in reserve", legSets[0])
	}

	pushStart := time.Date(2026, 10, 8, 18, 5, 0, 0, user.Location())
	push, err := svc.Between(ctx, user, timerange.Between(pushStart, pushStart.Add(2*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, s := range push {
		kinds[s.Kind]++
	}
	if len(push) != 5 || kinds[lift.KindWarmup] != 1 || kinds[lift.KindDrop] != 1 {
		t.Errorf("push day = %d sets, kinds %v", len(push), kinds)
	}
}

func TestImportHevyNamesUnmatchedAndRejectsOtherFiles(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "hevy2@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "H", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog := exercises.NewService(exercises.NewRepository(pool))
	svc := lifts.NewService(lifts.NewRepository(pool), catalog).
		WithImports(activity.NewService(activity.NewRepository(pool), nil), nil, catalog)

	csv := "title,start_time,end_time,exercise_title,set_type,weight_kg,reps\n" +
		`A,"1 Oct 2026, 10:00","1 Oct 2026, 11:00",Zercher Carry,normal,60,20` + "\n"
	res, err := svc.ImportHevy(ctx, user, strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if res.Sets != 1 || len(res.Unmatched) != 1 || res.Unmatched[0] != "Zercher Carry" {
		t.Errorf("result = %+v, want the carry kept as typed and named", res)
	}

	if _, err = svc.ImportHevy(ctx, user, strings.NewReader("Date,Workout Name,Exercise Name\n")); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("a Strong file = %v, want a validation error", err)
	}
}
