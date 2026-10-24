package profiles_test

import (
	"context"
	"net/http"
	"strings"
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

func TestProfilePageMonthlyCardFollowsAwardsSwitch(t *testing.T) {
	w := newWorld(t)
	leo, ana := person(t, w.pool, "leo-mc@n.test", "Leo"), person(t, w.pool, "ana-mc@n.test", "Ana")
	w.handle(t, leo, "leomc")
	do := w.pageClient(t)
	const card = `id="monthly-challenge"`

	w.share(t, leo, achievements.Sharing{Training: true, Awards: true})
	if body := do(ana, http.MethodGet, "/app/u/leomc").Body.String(); !strings.Contains(body, card) || !strings.Contains(body, "challenge") {
		t.Error("a follower does not see a shared monthly card")
	}
	if !strings.Contains(do(leo, http.MethodGet, "/app/u/leomc").Body.String(), card) {
		t.Error("the owner does not see their card")
	}

	w.share(t, leo, achievements.Sharing{Training: true, Awards: false})
	if strings.Contains(do(ana, http.MethodGet, "/app/u/leomc").Body.String(), card) {
		t.Error("the card shows with the awards switch off")
	}
	if !strings.Contains(do(leo, http.MethodGet, "/app/u/leomc").Body.String(), card) {
		t.Error("the owner lost their card when the switch went off")
	}
}

func TestProfileStripCarriesMonthlyMedals(t *testing.T) {
	w := newWorld(t)
	leo, ana := person(t, w.pool, "leo-ms@n.test", "Leo"), person(t, w.pool, "ana-ms@n.test", "Ana")
	w.handle(t, leo, "leoms")
	w.meetMonth(t, leo, "2026-09")
	body := newWorldBody(t, w, ana, "/app/u/leoms")
	if !strings.Contains(body, ">SEP<") {
		t.Error("the strip lacks the September coin")
	}
}

func newWorldBody(t *testing.T, w world, as users.User, path string) string {
	t.Helper()
	return w.pageClient(t)(as, http.MethodGet, path).Body.String()
}
