package profiles_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/users"
)

var (
	oct20 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	nov02 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
)

func (w world) meetMonth(t *testing.T, u users.User, key string) {
	t.Helper()
	ctx := context.Background()
	if _, err := w.pool.Exec(ctx, `INSERT INTO monthly_challenges (user_id, month, metric, target) VALUES ($1, ($2 || '-01')::date, 'workouts', 5)`, u.ID, key); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(ctx, `INSERT INTO achievements (user_id, category, kind, title, detail, occurred_at, source_key, private)
		VALUES ($1, 'awards', 'monthly_challenge_met', 'Challenge met', '', $2, $3, false)`, u.ID, oct20, key); err != nil {
		t.Fatal(err)
	}
}

func TestMonthlyFollowsAwardsSwitch(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, ana := person(t, w.pool, "leo-mo@n.test", "Leo"), person(t, w.pool, "ana-mo@n.test", "Ana")
	w.handle(t, leo, "leo")
	w.meetMonth(t, leo, "2026-09")

	w.share(t, leo, achievements.Sharing{Training: true, Awards: false})
	seen, err := w.svc.View(ctx, ana, "leo", oct20)
	if err != nil {
		t.Fatal(err)
	}
	if seen.MonthlyChallenge != nil || len(seen.Monthly) != 0 {
		t.Fatalf("a follower sees monthly with the switch off: %+v", seen)
	}
	own, err := w.svc.View(ctx, leo, "leo", oct20)
	if err != nil {
		t.Fatal(err)
	}
	if own.MonthlyChallenge == nil || own.MonthlyChallenge.Key != "2026-10" || len(own.Monthly) != 1 || own.Monthly[0].Key != "2026-09" {
		t.Fatalf("the owner's own view = %+v", own)
	}

	w.share(t, leo, achievements.Sharing{Training: true, Awards: true})
	seen, err = w.svc.View(ctx, ana, "leo", oct20)
	if err != nil {
		t.Fatal(err)
	}
	if seen.MonthlyChallenge == nil || len(seen.Monthly) != 1 {
		t.Fatalf("a follower sees no monthly with the switch on: %+v", seen)
	}
}

func TestProfileNeverShowsAMissedMonth(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, ana := person(t, w.pool, "leo-mm@n.test", "Leo"), person(t, w.pool, "ana-mm@n.test", "Ana")
	w.handle(t, leo, "leo")
	w.share(t, leo, achievements.Sharing{Awards: true})
	if _, err := w.pool.Exec(ctx, `INSERT INTO monthly_challenges (user_id, month, metric, target) VALUES ($1, '2026-10-01', 'checkin_days', 8)`, leo.ID); err != nil {
		t.Fatal(err)
	}
	v, err := w.svc.View(ctx, ana, "leo", nov02)
	if err != nil {
		t.Fatal(err)
	}
	if v.MonthlyChallenge == nil || v.MonthlyChallenge.Key != "2026-11" || len(v.Monthly) != 0 {
		t.Fatalf("view = %+v, want November's challenge and no October trace", v)
	}
}
