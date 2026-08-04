package checkins_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email, tz string) users.User {
	t.Helper()
	userSvc := users.NewService(users.NewRepository(pool))
	u, err := userSvc.Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     tz,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return u
}

func TestUpsertTodayIsOnePerDay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "fernando@north.test", "Europe/Lisbon")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	first, err := svc.UpsertToday(ctx, user, checkins.Input{Mood: 3, Energy: 3, Wins: "walked"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.UpsertToday(ctx, user, checkins.Input{Mood: 5, Energy: 4, Wins: "ran"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("same day should upsert, got %s vs %s", first.ID, second.ID)
	}
	if second.Mood != 5 || second.Wins != "ran" {
		t.Fatalf("upsert did not replace: %+v", second)
	}

	list, err := svc.List(ctx, user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 row, got %d", len(list))
	}
}

func TestOwnershipIsolation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "owner@north.test", "UTC")
	stranger := seedUser(t, pool, "stranger@north.test", "UTC")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	created, err := svc.UpsertToday(ctx, owner, checkins.Input{Mood: 4, Energy: 4})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Get(ctx, created.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger get: %v", err)
	}
	list, err := svc.List(ctx, stranger.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("stranger list should be empty, got %d", len(list))
	}
}

func TestValidateMoodEnergy(t *testing.T) {
	t.Parallel()
	_, err := checkins.Validate(checkins.Input{Mood: 0, Energy: 3})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var fe apperr.FieldErrors
	if !apperr.As(err, &fe) || fe.Messages()["mood"] == "" {
		t.Fatalf("want mood field error, got %v", err)
	}
}

func TestStreak(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "streak@north.test", "UTC")
	repo := checkins.NewRepository(pool)
	svc := checkins.NewService(repo, nil)

	today := checkins.LocalDate(user, time.Now())
	for i := 0; i < 3; i++ {
		day := today.AddDate(0, 0, -i)
		if _, err := repo.Upsert(ctx, user.ID, checkins.Write{
			LocalDate: day,
			Mood:      3,
			Energy:    3,
		}); err != nil {
			t.Fatal(err)
		}
	}

	streak, err := svc.Streak(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if streak != 3 {
		t.Fatalf("streak = %d, want 3", streak)
	}
}

func TestRelatedGoalMustBelongToUser(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "goalowner@north.test", "UTC")
	stranger := seedUser(t, pool, "goalstranger@north.test", "UTC")
	goalSvc := goals.NewService(goals.NewRepository(pool))
	svc := checkins.NewService(checkins.NewRepository(pool), goalSvc)

	g, err := goalSvc.Create(ctx, stranger.ID, goals.Input{
		Title:    "Stranger goal",
		Category: goals.CategoryFitness,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.UpsertToday(ctx, user, checkins.Input{
		Mood: 3, Energy: 3, RelatedGoalID: &g.ID,
	})
	if err == nil {
		t.Fatal("expected ownership error for foreign goal")
	}
}
