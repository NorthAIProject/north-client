package spend_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

func window() spend.Range {
	now := time.Now()
	return spend.Range{From: now.Add(-time.Hour), To: now.Add(time.Hour)}
}

// The ledger's whole purpose: what an account cost, split by what spent it.
func TestSpendIsTotalledPerUserModelAndSurface(t *testing.T) {
	pool := testdb.New(t)
	repo := spend.NewRepository(pool)
	userSvc := users.NewService(users.NewRepository(pool))
	ctx := context.Background()

	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "spender@north.test",
		PasswordHash: "x",
		DisplayName:  "Spender",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	repo.Record(ctx, spend.Generation{
		UserID: &user.ID, Surface: spend.SurfaceCoach,
		Provider: "openrouter", Model: "anthropic/claude-sonnet-4.5",
		InputTokens: 1000, OutputTokens: 500, CostMicros: 12_000,
	})
	repo.Record(ctx, spend.Generation{
		UserID: &user.ID, Surface: spend.SurfaceWeeklyReview,
		Provider: "openrouter", Model: "anthropic/claude-sonnet-4.5",
		InputTokens: 4000, OutputTokens: 800, CostMicros: 40_000,
	})

	byUser, err := repo.ByUser(ctx, window(), false)
	if err != nil {
		t.Fatalf("by user: %v", err)
	}
	if len(byUser) != 1 {
		t.Fatalf("got %d user rows, want 1", len(byUser))
	}
	if byUser[0].CostMicros != 52_000 {
		t.Errorf("CostMicros = %d, want 52000", byUser[0].CostMicros)
	}
	if byUser[0].Generations != 2 {
		t.Errorf("Generations = %d, want 2", byUser[0].Generations)
	}

	bySurface, err := repo.BySurface(ctx, window(), false)
	if err != nil {
		t.Fatalf("by surface: %v", err)
	}
	// Most expensive first, and the scheduled sweep outspending the chat is the
	// exact case a messages-derived figure would have missed entirely.
	if len(bySurface) != 2 || bySurface[0].Surface != spend.SurfaceWeeklyReview {
		t.Fatalf("surfaces = %+v, want weekly_review first", bySurface)
	}

	byModel, err := repo.ByModel(ctx, window(), false)
	if err != nil {
		t.Fatalf("by model: %v", err)
	}
	if len(byModel) != 1 || byModel[0].Model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("models = %+v, want one claude row", byModel)
	}
}

// A user's own key is their cost, not ours. Counting it would overstate COGS
// for precisely the accounts that cost us least.
func TestBillableOnlyExcludesBYOK(t *testing.T) {
	pool := testdb.New(t)
	repo := spend.NewRepository(pool)
	ctx := context.Background()

	repo.Record(ctx, spend.Generation{
		Surface: spend.SurfaceCoach, Provider: "openrouter", Model: "m",
		InputTokens: 10, OutputTokens: 10, CostMicros: 5_000,
	})
	repo.Record(ctx, spend.Generation{
		Surface: spend.SurfaceCoach, Provider: "openrouter", Model: "m",
		InputTokens: 10, OutputTokens: 10, CostMicros: 90_000, BYOK: true,
	})

	all, err := repo.BySurface(ctx, window(), false)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if all[0].CostMicros != 95_000 {
		t.Errorf("unfiltered CostMicros = %d, want 95000", all[0].CostMicros)
	}

	billable, err := repo.BySurface(ctx, window(), true)
	if err != nil {
		t.Fatalf("billable: %v", err)
	}
	if billable[0].CostMicros != 5_000 {
		t.Errorf("billable CostMicros = %d, want 5000; BYOK spend was counted as ours", billable[0].CostMicros)
	}
}

// A call with tokens but no price means the pricing table is missing a model,
// which makes every total an understatement. That has to be visible in the
// report rather than only in a log.
func TestUnpricedCallsAreCounted(t *testing.T) {
	pool := testdb.New(t)
	repo := spend.NewRepository(pool)
	ctx := context.Background()

	// No rate found: the real gap.
	repo.Record(ctx, spend.Generation{
		Surface: spend.SurfaceCoach, Provider: "mystery", Model: "",
		InputTokens: 100, OutputTokens: 50, CostMicros: 0, Priced: false,
	})
	repo.Record(ctx, spend.Generation{
		Surface: spend.SurfaceCoach, Provider: "openrouter", Model: "m",
		InputTokens: 100, OutputTokens: 50, CostMicros: 1_000, Priced: true,
	})
	// A BYOK call is not our gap even when it carries no price.
	repo.Record(ctx, spend.Generation{
		Surface: spend.SurfaceCoach, Provider: "openrouter", Model: "m",
		InputTokens: 100, OutputTokens: 50, CostMicros: 0, Priced: true, BYOK: true,
	})
	// The free floor: priced, and the price is zero. Counting this as a gap
	// would cry wolf on every report, because it is on the tail of every chain.
	repo.Record(ctx, spend.Generation{
		Surface: spend.SurfaceCoach, Provider: "openrouter", Model: "z-ai/glm-5.2:free",
		InputTokens: 900, OutputTokens: 200, CostMicros: 0, Priced: true,
	})

	n, err := repo.CountUnpriced(ctx, window())
	if err != nil {
		t.Fatalf("count unpriced: %v", err)
	}
	if n != 1 {
		t.Errorf("unpriced = %d, want 1; a zero-priced model was counted as a gap", n)
	}
}

// A call that belongs to no account is recorded as nobody's rather than
// attributed to someone, and must not break the per-user aggregate.
func TestAnUnattributedCallIsRecordedWithoutAUser(t *testing.T) {
	pool := testdb.New(t)
	repo := spend.NewRepository(pool)
	ctx := context.Background()

	repo.Record(ctx, spend.Generation{
		Surface: spend.SurfaceUnknown, Provider: "openrouter",
		InputTokens: 5, OutputTokens: 5, CostMicros: 100,
	})

	rows, err := repo.ByUser(ctx, window(), false)
	if err != nil {
		t.Fatalf("by user: %v", err)
	}
	if len(rows) != 1 || rows[0].UserID != nil {
		t.Fatalf("rows = %+v, want one row with a nil user", rows)
	}
}

func TestEurosRendersMicrosAsMoney(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		micros int64
		want   string
	}{
		{0, "0.00"},
		{1_000_000, "1.00"},
		{1_234_567, "1.23"},
		{1_235_000, "1.24"},      // rounds half up
		{999_995_000, "1000.00"}, // carries into the whole part
		{-2_500_000, "-2.50"},
	} {
		if got := spend.Euros(tc.micros); got != tc.want {
			t.Errorf("Euros(%d) = %q, want %q", tc.micros, got, tc.want)
		}
	}
}

var _ = uuid.Nil
