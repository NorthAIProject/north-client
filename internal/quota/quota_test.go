package quota_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

const testAction quota.Action = "coach_message"

// newService returns a service, one registered account, and the pool both sit
// on. The pool is returned because testdb.New creates a fresh database on every
// call: a second account has to be registered through this same pool, or it
// lands somewhere the service cannot see it.
func newService(t *testing.T, limits map[quota.Action]quota.Limit) (*quota.Service, users.User, *pgxpool.Pool) {
	t.Helper()

	pool := testdb.New(t)

	return quota.NewService(quota.NewRepository(pool), limits, nil), register(t, pool, "fernando@north.test", "Fernando Correia"), pool
}

func register(t *testing.T, pool *pgxpool.Pool, email, name string) users.User {
	t.Helper()

	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  name,
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}

func TestABudgetIsSpentAndThenRefused(t *testing.T) {
	svc, user, _ := newService(t, map[quota.Action]quota.Limit{
		testAction: {PerWindow: 3, Window: time.Hour},
	})
	ctx := context.Background()

	for i := range 3 {
		decision, err := svc.Consume(ctx, user.ID, testAction)
		if err != nil {
			t.Fatalf("consume %d: %v", i+1, err)
		}
		if !decision.Allowed {
			t.Fatalf("request %d refused while the budget of 3 should still cover it", i+1)
		}
	}

	decision, err := svc.Consume(ctx, user.ID, testAction)
	if err != nil {
		t.Fatalf("consume 4: %v", err)
	}
	if decision.Allowed {
		t.Error("the 4th request was allowed; a budget of 3 did not bound anything")
	}
}

// The reason the counter is keyed by account: one person exhausting their
// budget must not spend anybody else's.
func TestOneAccountExhaustedLeavesAnotherAccountUntouched(t *testing.T) {
	svc, noisy, pool := newService(t, map[quota.Action]quota.Limit{
		testAction: {PerWindow: 2, Window: time.Hour},
	})
	ctx := context.Background()
	quiet := register(t, pool, "someone.else@north.test", "Someone Else")

	for range 2 {
		if _, err := svc.Consume(ctx, noisy.ID, testAction); err != nil {
			t.Fatalf("setup consume: %v", err)
		}
	}
	decision, err := svc.Consume(ctx, noisy.ID, testAction)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if decision.Allowed {
		t.Fatal("setup failed: the noisy account was not actually exhausted")
	}

	decision, err = svc.Consume(ctx, quiet.ID, testAction)
	if err != nil {
		t.Fatalf("consume for the quiet account: %v", err)
	}
	if !decision.Allowed {
		t.Error("a second account was refused because the first spent its budget")
	}
}

// Each action carries its own budget, so a person who has used up their reports
// can still talk to the coach.
func TestExhaustingOneActionLeavesAnotherActionAvailable(t *testing.T) {
	const other quota.Action = "report_generate"

	svc, user, _ := newService(t, map[quota.Action]quota.Limit{
		testAction: {PerWindow: 1, Window: time.Hour},
		other:      {PerWindow: 1, Window: time.Hour},
	})
	ctx := context.Background()

	if _, err := svc.Consume(ctx, user.ID, testAction); err != nil {
		t.Fatalf("setup consume: %v", err)
	}
	decision, err := svc.Consume(ctx, user.ID, testAction)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if decision.Allowed {
		t.Fatal("setup failed: the first action was not exhausted")
	}

	decision, err = svc.Consume(ctx, user.ID, other)
	if err != nil {
		t.Fatalf("consume other action: %v", err)
	}
	if !decision.Allowed {
		t.Error("a second action was refused because the first was exhausted")
	}
}

// A refusal a client cannot act on is barely better than a hang: it has to be
// told when the budget comes back.
func TestARefusalSaysWhenTheBudgetReturns(t *testing.T) {
	svc, user, _ := newService(t, map[quota.Action]quota.Limit{
		testAction: {PerWindow: 1, Window: time.Hour},
	})
	ctx := context.Background()

	if _, err := svc.Consume(ctx, user.ID, testAction); err != nil {
		t.Fatalf("setup consume: %v", err)
	}

	decision, err := svc.Consume(ctx, user.ID, testAction)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if decision.Allowed {
		t.Fatal("setup failed: the budget was not exhausted")
	}
	if decision.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want a positive duration", decision.RetryAfter)
	}
	if decision.RetryAfter > time.Hour {
		t.Errorf("RetryAfter = %v, want no more than the window of 1h", decision.RetryAfter)
	}
}

// An action nobody configured a budget for is not secretly bounded at zero,
// which would take a working feature offline the moment it was guarded.
func TestAnActionWithNoConfiguredBudgetIsAllowed(t *testing.T) {
	svc, user, _ := newService(t, map[quota.Action]quota.Limit{})
	ctx := context.Background()

	for i := range 5 {
		decision, err := svc.Consume(ctx, user.ID, "never_configured")
		if err != nil {
			t.Fatalf("consume %d: %v", i+1, err)
		}
		if !decision.Allowed {
			t.Fatalf("request %d refused for an action with no configured budget", i+1)
		}
	}
}

// The limiter is not more important than the thing it protects. If the counter
// cannot be read, the request goes through and the operator finds out from the
// log, rather than every guarded page failing at once.
func TestACounterFailureFailsOpen(t *testing.T) {
	svc := quota.NewService(brokenCounter{}, map[quota.Action]quota.Limit{
		testAction: {PerWindow: 1, Window: time.Hour},
	}, nil)

	decision, err := svc.Consume(context.Background(), uuid.New(), testAction)
	if err != nil {
		t.Fatalf("a counter failure surfaced as an error instead of failing open: %v", err)
	}
	if !decision.Allowed {
		t.Error("a counter failure refused the request; a limiter outage became an application outage")
	}
}

type brokenCounter struct{}

func (brokenCounter) Consume(context.Context, uuid.UUID, quota.Action, time.Duration) (quota.Count, error) {
	return quota.Count{}, errors.New("counter unavailable")
}

func (brokenCounter) Sweep(context.Context, time.Time) error {
	return errors.New("counter unavailable")
}
