package decisions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/decisions"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func decidedAt(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE decisions SET decided_at = $2 WHERE id = $1`, id, at); err != nil {
		t.Fatal(err)
	}
}

// A call comes back at 30 days; answering it holds the question until the
// 90-day mark, when it comes back once more.
func TestRevisitAt30And90Days(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "revisit@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))
	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	d, err := svc.Create(ctx, user.ID, decisions.Input{Title: "Run the half in March"})
	if err != nil {
		t.Fatal(err)
	}
	decidedAt(t, pool, d.ID, now.AddDate(0, 0, -20))
	if _, ok, _ := svc.DueRevisit(ctx, user.ID, now); ok {
		t.Fatal("asked before 30 days")
	}

	decidedAt(t, pool, d.ID, now.AddDate(0, 0, -31))
	due, ok, err := svc.DueRevisit(ctx, user.ID, now)
	if err != nil || !ok || due.ID != d.ID || due.Mark != 30 {
		t.Fatalf("due = %+v, %v, %v", due, ok, err)
	}

	if _, err = svc.Update(ctx, d.ID, user.ID, decisions.Input{Title: d.Title, Outcome: "The knee held up.", Held: "yes"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ = svc.DueRevisit(ctx, user.ID, now); ok {
		t.Fatal("asked again after answering")
	}

	// Fast forward: the answer was given at day 31, before the 90-day mark.
	if _, err = pool.Exec(ctx, `UPDATE decisions SET decided_at = $2, held_at = $3 WHERE id = $1`,
		d.ID, now.AddDate(0, 0, -91), now.AddDate(0, 0, -60)); err != nil {
		t.Fatal(err)
	}
	due, ok, _ = svc.DueRevisit(ctx, user.ID, now)
	if !ok || due.Mark != 90 {
		t.Fatalf("90-day revisit = %+v, %v", due, ok)
	}

	c, err := svc.Calibration(ctx, user.ID)
	if err != nil || c.Yes != 1 || c.Revisited() != 1 {
		t.Fatalf("calibration = %+v, %v", c, err)
	}
}

// A decision logged long before revisits existed is not asked about.
func TestOldMarksAreLetGo(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "old-revisit@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))
	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	d, _ := svc.Create(ctx, user.ID, decisions.Input{Title: "Move to Lisbon"})
	decidedAt(t, pool, d.ID, now.AddDate(0, 0, -200))
	if _, ok, _ := svc.DueRevisit(ctx, user.ID, now); ok {
		t.Fatal("asked about a decision from months ago")
	}
}

func TestHeldMustBeAnAnswer(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "held@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))
	d, _ := svc.Create(ctx, user.ID, decisions.Input{Title: "Take the offer"})
	if _, err := svc.Update(ctx, d.ID, user.ID, decisions.Input{Title: d.Title, Held: "maybe"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v", err)
	}
}
