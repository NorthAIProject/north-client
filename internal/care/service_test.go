package care_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/care"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "care@north.test",
		PasswordHash: "$2a$12$test",
		DisplayName:  "Care",
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
	goalSvc := goals.NewService(goals.NewRepository(pool))
	svc := care.NewService(care.Options{
		Reminders: meals.NewMealReminderService(meals.NewRepository(pool)),
		CheckIns:  checkins.NewService(checkins.NewRepository(pool), goalSvc),
		Hydration: hydration.NewService(hydration.NewRepository(pool)),
		Sleep:     sleep.NewService(sleep.NewRepository(pool)),
		Habits:    habits.NewService(habits.NewRepository(pool)),
	})

	snap, err := svc.Load(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.HydrationSeries) != 7 {
		t.Fatalf("hydration series = %d", len(snap.HydrationSeries))
	}
	if snap.HasHydrationChart() {
		t.Fatal("expected empty hydration chart")
	}
}

func TestLoadHydrationGapFill(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool)
	goalSvc := goals.NewService(goals.NewRepository(pool))
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))
	svc := care.NewService(care.Options{
		Reminders: meals.NewMealReminderService(meals.NewRepository(pool)),
		CheckIns:  checkins.NewService(checkins.NewRepository(pool), goalSvc),
		Hydration: hydrationSvc,
		Sleep:     sleep.NewService(sleep.NewRepository(pool)),
		Habits:    habits.NewService(habits.NewRepository(pool)),
	})

	if _, err := hydrationSvc.Log(ctx, user, hydration.Glass); err != nil {
		t.Fatal(err)
	}

	snap, err := svc.Load(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	zeros := 0
	for _, d := range snap.HydrationSeries {
		if d.Value == 0 {
			zeros++
		}
	}
	if zeros != 6 {
		t.Fatalf("zero-fill = %d want 6", zeros)
	}
	if !snap.HasHydrationChart() {
		t.Fatal("expected hydration data")
	}
}
