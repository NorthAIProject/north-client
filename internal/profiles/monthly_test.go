package profiles_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/awards"
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
	// The challenge exists once its owner has opened the month; a viewer only reads it.
	if _, err := w.svc.View(ctx, leo, "leo", nov02); err != nil {
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

// Looking at someone's profile must not write their data: the row fixes their
// target for the month, so only their own visit may create it.
func TestViewingAnotherProfileNeverCreatesTheirChallenge(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, ana := person(t, w.pool, "leo-pk@n.test", "Leo"), person(t, w.pool, "ana-pk@n.test", "Ana")
	w.handle(t, leo, "leopk")
	w.share(t, leo, achievements.Sharing{Training: true, Awards: true})
	count := func() int {
		t.Helper()
		var n int
		if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM monthly_challenges WHERE user_id = $1`, leo.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	seen, err := w.svc.View(ctx, ana, "leopk", oct20)
	if err != nil {
		t.Fatal(err)
	}
	if seen.MonthlyChallenge != nil || count() != 0 {
		t.Fatalf("a viewer's visit left card=%v rows=%d", seen.MonthlyChallenge, count())
	}

	own, err := w.svc.View(ctx, leo, "leopk", oct20)
	if err != nil {
		t.Fatal(err)
	}
	if own.MonthlyChallenge == nil || count() != 1 {
		t.Fatalf("the owner's own visit did not create it: %v rows=%d", own.MonthlyChallenge, count())
	}
	seen, err = w.svc.View(ctx, ana, "leopk", oct20)
	if err != nil {
		t.Fatal(err)
	}
	if seen.MonthlyChallenge == nil || seen.MonthlyChallenge.Key != "2026-10" {
		t.Fatalf("a viewer does not see the existing challenge: %v", seen.MonthlyChallenge)
	}
}

// brokenMonthly is the real awards service whose monthly reads fail.
type brokenMonthly struct{ *awards.Service }

func (brokenMonthly) MonthlyMedals(context.Context, uuid.UUID) ([]awards.MonthlyMedal, error) {
	return nil, errors.New("monthly store down")
}

func (brokenMonthly) Monthly(context.Context, uuid.UUID, time.Time) (awards.MonthlyChallenge, error) {
	return awards.MonthlyChallenge{}, errors.New("monthly store down")
}

func (brokenMonthly) PeekMonthly(context.Context, uuid.UUID, time.Time) (awards.MonthlyChallenge, bool, error) {
	return awards.MonthlyChallenge{}, false, errors.New("monthly store down")
}

// A failed monthly read hides the card; the profile (and its badges) still load.
func TestProfileSurvivesAFailedMonthlyRead(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, ana := person(t, w.pool, "leo-bk@n.test", "Leo"), person(t, w.pool, "ana-bk@n.test", "Ana")
	w.handle(t, leo, "leobk")
	awards.NewService(awards.NewRepository(w.pool)).Grant(ctx, leo.ID, time.Now().Add(-time.Hour), "workouts_1")
	w.svc.WithAwards(brokenMonthly{awards.NewService(awards.NewRepository(w.pool))})

	for _, viewer := range []users.User{ana, leo} {
		v, err := w.svc.View(ctx, viewer, "leobk", oct20)
		if err != nil {
			t.Fatalf("View failed on a monthly error: %v", err)
		}
		if v.MonthlyChallenge != nil || len(v.Monthly) != 0 || len(v.Awards) != 1 {
			t.Fatalf("view = %+v, want badges only", v)
		}
	}
}
