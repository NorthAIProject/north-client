package fitness_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/fitness"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

func seedUser(t *testing.T, pool *pgxpool.Pool) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "fitness-hub@north.test",
		PasswordHash: "$2a$12$test",
		DisplayName:  "Fitness",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLoadEmptyUser(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool)

	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	calculatorSvc := calculator.NewService(calculator.NewRepository(pool), biometricSvc)
	activitySvc := activity.NewService(activity.NewRepository(pool), biometricSvc)
	foodLogSvc := meals.NewFoodLogService(meals.NewRepository(pool))
	mealProgressSvc := meals.NewTrackMealProgressService(foodLogSvc, calculatorSvc)

	svc := fitness.NewService(fitness.Options{
		Activity: activitySvc,
		Workouts: workouts.NewService(workouts.Options{Repository: workouts.NewRepository(pool)}),
		Strava: strava.NewService(strava.Options{
			// No sealer: this exercises the hub, and the unencrypted path is
			// still a supported deployment. Encryption has its own tests.
			Repository: strava.NewRepository(pool, nil),
			Activity:   activitySvc,
			Biometrics: biometricSvc,
		}),
		Meals: mealProgressSvc,
	})

	snap, err := svc.Load(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.CalorieSeries) != 7 {
		t.Fatalf("calorie series = %d want 7", len(snap.CalorieSeries))
	}
	if snap.HasCalorieChart() {
		t.Fatal("expected empty calorie chart")
	}
	if snap.NextSession != nil {
		t.Fatal("expected no next session")
	}
	if snap.HasMealProgress {
		t.Fatal("expected no meal progress without macro goal")
	}
}

// The hub is where a person checks whether their device is actually feeding
// Khepri anything. Readings that exist in the database but never reach the
// snapshot are invisible to them.
func TestLoadSurfacesRecentDeviceReadings(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool)

	healthSvc := health.NewService(health.NewRepository(pool))
	if _, err := healthSvc.Ingest(context.Background(), user.ID, "apple_health", []health.Reading{
		{Metric: "steps", Value: 8432, Unit: "count", StartedAt: time.Now().Add(-24 * time.Hour)},
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	svc := fitness.NewService(fitness.Options{Health: healthSvc})

	snap, err := svc.Load(context.Background(), user)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !snap.HasDeviceReadings {
		t.Fatalf("HasDeviceReadings = false, want true (readings: %v)", snap.DeviceReadings)
	}
	if len(snap.DeviceReadings) == 0 {
		t.Error("DeviceReadings is empty; the step count never reached the page")
	}
}

func TestLoadReportsNoDeviceReadingsForAnUntrackedUser(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool)

	svc := fitness.NewService(fitness.Options{Health: health.NewService(health.NewRepository(pool))})

	snap, err := svc.Load(context.Background(), user)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if snap.HasDeviceReadings {
		t.Errorf("HasDeviceReadings = true for a user with no device: %v", snap.DeviceReadings)
	}
}
