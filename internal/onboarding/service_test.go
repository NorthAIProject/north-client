package onboarding_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !u.NeedsOnboarding() {
		t.Fatal("new user should need onboarding")
	}
	return u
}

func newSvc(pool *pgxpool.Pool) *onboarding.Service {
	return onboarding.NewService(
		users.NewService(users.NewRepository(pool)),
		memories.NewService(memories.NewRepository(pool)),
		goals.NewService(goals.NewRepository(pool)),
	)
}

func TestCompleteSeedsContext(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-complete@north.test")
	svc := newSvc(pool)

	updated, err := svc.Complete(ctx, user, onboarding.Answers{
		FocusAreas:    []string{lifedomain.Fitness, lifedomain.Health},
		CoachingStyle: onboarding.StyleText(onboarding.StyleDirect),
		NearTermGoal:  "Run a 10K",
		GoalCategory:  lifedomain.Fitness,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NeedsOnboarding() {
		t.Fatal("expected onboarded user")
	}
	if updated.CoachingStyle != onboarding.StyleText(onboarding.StyleDirect) {
		t.Fatalf("coaching style = %q", updated.CoachingStyle)
	}

	userSvc := users.NewService(users.NewRepository(pool))
	reloaded, err := userSvc.ByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.OnboardedAt == nil {
		t.Fatal("onboarded_at not set in database")
	}

	memSvc := memories.NewService(memories.NewRepository(pool))
	memList, err := memSvc.ListApproved(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(memList) < 3 {
		t.Fatalf("expected at least 3 memories (2 focus + coaching), got %d", len(memList))
	}

	goalSvc := goals.NewService(goals.NewRepository(pool))
	active, err := goalSvc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("active goals = %d", len(active))
	}
	if active[0].Title != "Run a 10K" {
		t.Fatalf("goal title = %q", active[0].Title)
	}
	if active[0].Category != lifedomain.Fitness {
		t.Fatalf("goal category = %q", active[0].Category)
	}

	ctxMems, err := memSvc.ForContext(ctx, user.ID, "fitness coaching")
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxMems) == 0 {
		t.Fatal("expected seeded memories in coach context")
	}
}

func TestSkipMarksOnboardedOnly(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-skip@north.test")
	svc := newSvc(pool)

	updated, err := svc.Skip(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NeedsOnboarding() {
		t.Fatal("expected onboarded after skip")
	}

	memSvc := memories.NewService(memories.NewRepository(pool))
	memList, err := memSvc.ListApproved(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(memList) != 0 {
		t.Fatalf("skip should not seed memories, got %d", len(memList))
	}

	goalSvc := goals.NewService(goals.NewRepository(pool))
	active, err := goalSvc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("skip should not seed goals, got %d", len(active))
	}
}

func TestCompleteIdempotent(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-idempotent@north.test")
	svc := newSvc(pool)

	answers := onboarding.Answers{
		FocusAreas:    []string{lifedomain.Work},
		CoachingStyle: onboarding.StyleText(onboarding.StyleSupportive),
		NearTermGoal:  "Ship the feature",
		GoalCategory:  lifedomain.Work,
	}

	first, err := svc.Complete(ctx, user, answers)
	if err != nil {
		t.Fatal(err)
	}

	second, err := svc.Complete(ctx, first, answers)
	if err != nil {
		t.Fatal(err)
	}
	if second.NeedsOnboarding() {
		t.Fatal("still onboarded after second complete")
	}

	goalSvc := goals.NewService(goals.NewRepository(pool))
	active, err := goalSvc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("idempotent complete should not duplicate goals, got %d", len(active))
	}
}

func TestValidateAnswers(t *testing.T) {
	_, err := onboarding.ValidateAnswers(nil, "", "", "")
	if err == nil {
		t.Fatal("expected validation error for empty form")
	}

	answers, err := onboarding.ValidateAnswers(
		[]string{lifedomain.Personal},
		onboarding.StyleDirect,
		"",
		"Read twelve books",
	)
	if err != nil {
		t.Fatal(err)
	}
	if answers.CoachingStyle != onboarding.StyleText(onboarding.StyleDirect) {
		t.Fatalf("style = %q", answers.CoachingStyle)
	}
	if answers.GoalCategory != lifedomain.Personal {
		t.Fatalf("category = %q", answers.GoalCategory)
	}
}
