package mind_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*mind.Service, *checkins.Service, users.User) {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	checkinSvc := checkins.NewService(checkins.NewRepository(pool), nil)
	return mind.NewService(mind.NewRepository(pool), checkinSvc), checkinSvc, user
}

func TestCreateAndRecent(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	mood := 4
	created, err := svc.Create(ctx, user.ID, mind.Input{Content: "Felt good on the run today.", Mood: &mood})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Mood == nil || *created.Mood != 4 {
		t.Fatalf("mood = %v, want 4", created.Mood)
	}

	recent, err := svc.Recent(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(recent))
	}
}

func TestValidationRejectsEmptyContent(t *testing.T) {
	t.Parallel()

	if _, err := mind.Validate(mind.Input{}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestValidationRejectsOutOfRangeMood(t *testing.T) {
	t.Parallel()

	bad := 9
	if _, err := mind.Validate(mind.Input{Content: "x", Mood: &bad}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestMoodTrendAveragesRecentCheckIns(t *testing.T) {
	svc, checkinSvc, user := newService(t)
	ctx := context.Background()

	if _, err := checkinSvc.UpsertToday(ctx, user, checkins.Input{Mood: 4, Energy: 3}); err != nil {
		t.Fatalf("check in: %v", err)
	}

	trend, err := svc.RecentMoodTrend(ctx, user.ID, 14)
	if err != nil {
		t.Fatalf("recent mood trend: %v", err)
	}
	if trend.Count != 1 {
		t.Fatalf("count = %d, want 1", trend.Count)
	}
	if trend.AverageMood != 4 || trend.AverageEnergy != 3 {
		t.Fatalf("trend = %+v, want mood 4 / energy 3", trend)
	}
}

func TestMoodTrendIsZeroWithoutAnyCheckIns(t *testing.T) {
	svc, _, user := newService(t)

	trend, err := svc.RecentMoodTrend(context.Background(), user.ID, 14)
	if err != nil {
		t.Fatalf("recent mood trend: %v", err)
	}
	if trend.Count != 0 {
		t.Fatalf("count = %d, want 0", trend.Count)
	}
}
