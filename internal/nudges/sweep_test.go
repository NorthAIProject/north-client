package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func TestSweepEvaluatesOnboardedUsersOnly(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	eligible := mustOnboard(t, pool, seedUser(t, pool, "sweep-yes@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, eligible.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	skipped := seedUser(t, pool, "sweep-no@north.test")

	svc := evalService(pool, now)
	if err := nudges.NewSweeper(svc, nil).HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}

	open, err := svc.ListOpen(ctx, eligible.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("eligible open = %d, want 1", len(open))
	}

	skippedOpen, err := svc.ListOpen(ctx, skipped.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(skippedOpen) != 0 {
		t.Fatalf("not-onboarded user was nudged: %#v", skippedOpen)
	}
}

func TestSweepIsIdempotentWithinTheDedupeWindow(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	user := mustOnboard(t, pool, seedUser(t, pool, "sweep-once@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

	svc := evalService(pool, now)
	sweep := nudges.NewSweeper(svc, nil)
	if err := sweep.HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := sweep.HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}

	open, err := svc.ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("after two sweeps, open = %d, want 1", len(open))
	}
}
