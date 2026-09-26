package stats_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/caffeine"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestServiceReadsRealRows(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "stats@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "T", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	sleepSvc := sleep.NewService(sleep.NewRepository(pool))
	caffeineSvc := caffeine.NewService(caffeine.NewRepository(pool))
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	activitySvc := activity.NewService(activity.NewRepository(pool), biometricSvc)
	if _, err = biometricSvc.Record(ctx, user.ID, biometrics.Input{
		WeightKg: 80, HeightCm: 180, DateOfBirth: time.Now().AddDate(-30, 0, 0), Sex: biometrics.SexMale,
	}); err != nil {
		t.Fatal(err)
	}

	// Six nights: the three after an evening coffee are an hour shorter.
	today := timerange.StartOfDay(time.Now().UTC())
	for i := 1; i <= 6; i++ {
		morning := today.AddDate(0, 0, -i)
		minutes := 480
		if i%2 == 0 {
			minutes = 420
			evening := morning.Add(-6 * time.Hour) // 18:00 the day before
			if _, err = caffeineSvc.Log(ctx, user, caffeine.LogInput{Preset: "coffee", At: &evening}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = sleepSvc.LogFor(ctx, user, morning, sleep.Input{DurationMinutes: minutes}); err != nil {
			t.Fatalf("log sleep: %v", err)
		}
	}
	started := today.AddDate(0, 0, -2).Add(7 * time.Hour)
	if _, err = activitySvc.Log(ctx, user.ID, activity.LogInput{
		ActivityCode: "running_11_3kmh", StartedAt: started, Duration: 25 * time.Minute, DistanceM: 5000,
	}); err != nil {
		t.Fatalf("log run: %v", err)
	}

	svc := stats.NewService(stats.Sources{Sleep: sleepSvc, Caffeine: caffeineSvc, Activity: activitySvc})
	month := timerange.Parse(timerange.KeyMonth, time.UTC)

	sl, err := svc.Sleep(ctx, user, month)
	if err != nil {
		t.Fatal(err)
	}
	if len(sl.Nights) != 6 || sl.AvgMinutes != 450 {
		t.Errorf("sleep = %d nights, avg %d", len(sl.Nights), sl.AvgMinutes)
	}

	cardio, err := svc.Cardio(ctx, user, month)
	if err != nil {
		t.Fatal(err)
	}
	if cardio.Runs.Count != 1 || cardio.Runs.Best5K != 1500 {
		t.Errorf("runs = %+v", cardio.Runs)
	}

	found, days, err := svc.Patterns(ctx, user, month)
	if err != nil {
		t.Fatal(err)
	}
	if days < stats.PatternsWindow {
		t.Errorf("patterns read %d days, want at least %d", days, stats.PatternsWindow)
	}
	if len(found) == 0 || found[0].Key != "sleep_late_caffeine" || found[0].Diff != -60 {
		t.Errorf("findings = %+v", found)
	}
}
