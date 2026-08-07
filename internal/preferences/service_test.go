package preferences_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*preferences.Service, users.User) {
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

	return preferences.NewService(preferences.NewRepository(pool)), user
}

func TestGetReturnsDefaultsBeforeAnyUpsert(t *testing.T) {
	svc, user := newService(t)

	p, err := svc.Get(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("get should never fail for an unconfigured account: %v", err)
	}
	if p.UnitsSystem != preferences.UnitsMetric {
		t.Fatalf("units_system = %q, want metric default", p.UnitsSystem)
	}
	if p.DefaultGoal != calculator.GoalMaintenance {
		t.Fatalf("default_goal = %q, want maintenance default", p.DefaultGoal)
	}
}

func TestUpsertSavesAndOverwrites(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, user.ID, preferences.Input{
		UnitsSystem: preferences.UnitsImperial, DefaultGoal: calculator.GoalCutting, DefaultMacroSplit: calculator.SplitLowCarb,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	p, err := svc.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.UnitsSystem != preferences.UnitsImperial || p.DefaultGoal != calculator.GoalCutting {
		t.Fatalf("preferences did not persist: %+v", p)
	}

	// A second upsert overwrites in place — no history to accumulate.
	if _, err := svc.Upsert(ctx, user.ID, preferences.Input{UnitsSystem: preferences.UnitsMetric}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	p, err = svc.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get after second upsert: %v", err)
	}
	if p.UnitsSystem != preferences.UnitsMetric {
		t.Fatalf("units_system = %q, want metric after overwrite", p.UnitsSystem)
	}
	// Explicit empty fields on the second upsert fall back to their own
	// defaults rather than preserving the first upsert's values — Input has
	// no notion of "leave unchanged."
	if p.DefaultGoal != calculator.GoalMaintenance {
		t.Fatalf("default_goal = %q, want maintenance default (Input has no partial-update semantics)", p.DefaultGoal)
	}
}

func TestValidationRejectsUnknownEnums(t *testing.T) {
	t.Parallel()

	if _, err := preferences.Validate(preferences.Input{UnitsSystem: "furlongs"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
