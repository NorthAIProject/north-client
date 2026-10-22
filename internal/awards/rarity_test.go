package awards_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/awards/award"
)

func TestRarityLabel(t *testing.T) {
	for _, c := range []struct {
		earned, active int
		want           string
	}{{0, 0, ""}, {0, 5, "0%"}, {1, 300, "<1%"}, {1, 3, "33%"}, {3, 3, "100%"}, {5, 3, "100%"}} {
		if got := (awards.Rarity{Earned: c.earned, Active: c.active}).Label(); got != c.want {
			t.Errorf("%d of %d = %q, want %q", c.earned, c.active, got, c.want)
		}
	}
}

func TestComputeRarity(t *testing.T) {
	e := newEnv(t, "UTC")
	ctx := context.Background()
	e.checkIns(0, 0) // old: 2026-01-01 is a row, but created_at is now
	e.svc.Grant(ctx, e.user, now, "listened_to_body")
	if err := e.svc.ComputeRarity(ctx, now); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Rarities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(award.Catalog()) {
		t.Fatalf("%d rarities, want %d", len(got), len(award.Catalog()))
	}
	r := got["listened_to_body"]
	if r.Earned != 1 || got["workouts_1"].Earned != 0 {
		t.Fatalf("rarity = %+v", got)
	}
}
