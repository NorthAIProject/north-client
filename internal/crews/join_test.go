package crews_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	crewsdb "github.com/NorthAIProject/north-client/internal/crews/db"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/social"
)

// A block between the joiner and the owner keeps the joiner out, whichever
// way it points, and the refusal does not say why.
func TestJoinRefusedWhenBlocked(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	blocks := social.NewRepository(pool)
	svc := service(pool).WithBlocks(blocks)
	ana := person(t, pool, "ana@north.test", "Ana")
	leo := person(t, pool, "leo@north.test", "Leo")
	c, _ := svc.Create(ctx, ana.ID, "Iron rats")

	for _, block := range []struct{ by, of uuid.UUID }{{ana.ID, leo.ID}, {leo.ID, ana.ID}} {
		if err := blocks.Block(ctx, block.by, block.of); err != nil {
			t.Fatal(err)
		}
		_, err := svc.Join(ctx, leo.ID, c.Code)
		if got := fieldError(err, "crew"); got != "You can't join this crew." {
			t.Fatalf("join across a block = %v", err)
		}
		if n := members(t, pool, c.ID); n != 1 {
			t.Fatalf("%d members after a refused join", n)
		}
		if err = blocks.Unblock(ctx, block.by, block.of); err != nil {
			t.Fatal(err)
		}
	}
}

// 25 people for a free owner's crew, 100 for a Pro owner's.
func TestJoinCaps(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := service(pool)
	free := person(t, pool, "free@north.test", "Free")
	pro := person(t, pool, "pro@north.test", "Pro")
	if _, err := pool.Exec(ctx, `UPDATE users SET tier = 'pro' WHERE id = $1`, pro.ID); err != nil {
		t.Fatal(err)
	}
	freeCrew, _ := svc.Create(ctx, free.ID, "Free")
	proCrew, _ := svc.Create(ctx, pro.ID, "Pro")

	q := crewsdb.New(pool)
	for i := 1; i < crew.MaxMembers; i++ {
		u := person(t, pool, fmt.Sprintf("m%d@north.test", i), fmt.Sprintf("M%d", i))
		for _, id := range []uuid.UUID{freeCrew.ID, proCrew.ID} {
			if _, err := q.AddMember(ctx, crewsdb.AddMemberParams{CrewID: id, UserID: u.ID}); err != nil {
				t.Fatal(err)
			}
		}
	}

	late := person(t, pool, "late@north.test", "Late")
	if _, err := svc.Join(ctx, late.ID, freeCrew.Code); fieldError(err, "crew") != "This crew is full." {
		t.Fatalf("member %d joined a free crew: %v", crew.MaxMembers+1, err)
	}
	if _, err := svc.Join(ctx, late.ID, proCrew.Code); err != nil {
		t.Fatalf("member %d could not join a Pro crew: %v", crew.MaxMembers+1, err)
	}
	if crew.MaxMembers != 25 || crew.MaxMembersPro != 100 {
		t.Fatalf("caps = %d / %d; want 25 / 100", crew.MaxMembers, crew.MaxMembersPro)
	}
}

func members(t *testing.T, pool *pgxpool.Pool, crewID uuid.UUID) int {
	t.Helper()
	n, err := crewsdb.New(pool).CountMembers(context.Background(), crewID)
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}
