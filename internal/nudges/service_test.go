package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newStore(pool *pgxpool.Pool) *nudges.Service {
	return nudges.NewService(nudges.NewRepository(pool), nil, nil, nil)
}

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCreateIfAbsentInsertsOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "nudge-once@north.test")
	svc := newStore(pool)

	draft := nudges.Draft{
		Kind:      nudges.KindMissedCheckIn,
		DedupeKey: "2026-08-15",
		Title:     "Check in with yourself",
		Body:      "It has been 3 days since your last check-in.",
		Href:      "/app/check-ins",
	}

	first, created, err := svc.CreateIfAbsent(ctx, user.ID, draft)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first insert should create a row")
	}
	if first.ID.String() == "" {
		t.Fatal("created nudge needs an id")
	}

	_, created, err = svc.CreateIfAbsent(ctx, user.ID, draft)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("same dedupe key must not insert a second row")
	}

	list, err := svc.ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %d, want 1", len(list))
	}
}

func TestListOpenHidesDismissed(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "nudge-dismiss@north.test")
	svc := newStore(pool)

	n, _, err := svc.CreateIfAbsent(ctx, user.ID, nudges.Draft{
		Kind:      nudges.KindMissedCheckIn,
		DedupeKey: "2026-08-15",
		Title:     "Check in with yourself",
		Body:      "It has been 3 days since your last check-in.",
		Href:      "/app/check-ins",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.Dismiss(ctx, n.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("dismissed nudge still open: %#v", list)
	}
}

func TestMarkReadAndDismissAreOwned(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "nudge-owner@north.test")
	stranger := seedUser(t, pool, "nudge-stranger@north.test")
	svc := newStore(pool)

	n, _, err := svc.CreateIfAbsent(ctx, owner.ID, nudges.Draft{
		Kind:      nudges.KindGoalDeadline,
		DedupeKey: "goal:2026-08-20",
		Title:     "Run a 10K is due Wednesday",
		Body:      "Due in 5 days.",
		Href:      "/app/goals",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.MarkRead(ctx, n.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger read: %v", err)
	}
	if _, err = svc.Dismiss(ctx, n.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger dismiss: %v", err)
	}

	if _, err = svc.MarkRead(ctx, n.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCountUnreadIgnoresReadAndDismissed(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "nudge-unread@north.test")
	svc := newStore(pool)

	unread, _, err := svc.CreateIfAbsent(ctx, user.ID, nudges.Draft{
		Kind:      nudges.KindMissedCheckIn,
		DedupeKey: "2026-08-15",
		Title:     "Check in with yourself",
		Body:      "It has been 3 days since your last check-in.",
		Href:      "/app/check-ins",
	})
	if err != nil {
		t.Fatal(err)
	}
	read, _, err := svc.CreateIfAbsent(ctx, user.ID, nudges.Draft{
		Kind:      nudges.KindGoalDeadline,
		DedupeKey: "goal-a:2026-08-20",
		Title:     "A is due Wednesday",
		Body:      "Due in 5 days.",
		Href:      "/app/goals",
	})
	if err != nil {
		t.Fatal(err)
	}
	gone, _, err := svc.CreateIfAbsent(ctx, user.ID, nudges.Draft{
		Kind:      nudges.KindGoalDeadline,
		DedupeKey: "goal-b:2026-08-21",
		Title:     "B is due Thursday",
		Body:      "Due in 6 days.",
		Href:      "/app/goals",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.MarkRead(ctx, read.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Dismiss(ctx, gone.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	n, err := svc.CountUnread(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("unread = %d, want 1 (only %s)", n, unread.ID)
	}
}

func freeze(t time.Time) func() time.Time { return func() time.Time { return t } }

func evalService(pool *pgxpool.Pool, now time.Time) *nudges.Service {
	return nudges.NewService(
		nudges.NewRepository(pool),
		users.NewService(users.NewRepository(pool)),
		checkins.NewService(checkins.NewRepository(pool), nil),
		goals.NewService(goals.NewRepository(pool)),
	).WithClock(freeze(now))
}

func mustOnboard(t *testing.T, pool *pgxpool.Pool, user users.User, at time.Time) users.User {
	t.Helper()
	ctx := context.Background()
	userSvc := users.NewService(users.NewRepository(pool))
	if _, err := userSvc.MarkOnboarded(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET onboarded_at = $2 WHERE id = $1`, user.ID, at); err != nil {
		t.Fatal(err)
	}
	u, err := userSvc.ByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func writeCheckIn(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, day time.Time) {
	t.Helper()
	if _, err := checkins.NewRepository(pool).Upsert(context.Background(), userID, checkins.Write{
		LocalDate: day,
		Mood:      3,
		Energy:    3,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateMissedCheckInAfterTwoQuietDays(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "eval-missed@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

	n, err := evalService(pool, now).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("created = %d, want 1", n)
	}
	list, err := evalService(pool, now).ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != nudges.KindMissedCheckIn {
		t.Fatalf("open = %#v", list)
	}
}

func TestEvaluateSkipsRecentCheckIn(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "eval-recent@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC))

	n, err := evalService(pool, now).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d, want 0", n)
	}
}

func TestEvaluateSkipsNewAccounts(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "eval-new@north.test"), time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC))

	n, err := evalService(pool, now).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("brand-new account was nudged: created = %d", n)
	}
}

func TestEvaluateMissedCheckInIsDedupedOnTheSameLocalDay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "eval-dedupe@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	svc := evalService(pool, now)

	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second evaluate created %d, want 0", n)
	}
	list, err := svc.ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %d, want 1", len(list))
	}
}

func TestEvaluateDeadlineWithinSevenDays(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "eval-due@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))

	g, err := goals.NewService(goals.NewRepository(pool)).Create(ctx, user.ID, goals.Input{
		Title:      "Run a 10K",
		Category:   goals.CategoryFitness,
		TargetDate: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	n, err := evalService(pool, now).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("created = %d, want 1", n)
	}
	list, err := evalService(pool, now).ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != nudges.KindGoalDeadline {
		t.Fatalf("open = %#v", list)
	}
	if list[0].DedupeKey != g.ID.String()+":2026-08-20" {
		t.Fatalf("dedupe key = %q", list[0].DedupeKey)
	}
}

func TestEvaluateIgnoresOpenEndedAndOverdueGoals(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "eval-ignore-goal@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	goalSvc := goals.NewService(goals.NewRepository(pool))

	if _, err := goalSvc.Create(ctx, user.ID, goals.Input{
		Title:    "Open ended",
		Category: goals.CategoryFitness,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := goals.NewRepository(pool).Create(ctx, user.ID, goals.NewGoal{
		Title:      "Already late",
		Category:   goals.CategoryFitness,
		TargetDate: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	n, err := evalService(pool, now).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d, want 0", n)
	}
}

func TestEvaluateUsesTheUsersTimezoneForLocalDay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 0, 30, 0, 0, time.UTC)
	svc := evalService(pool, now)

	utc := mustOnboard(t, pool, seedUser(t, pool, "eval-utc@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, utc.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

	lisbonUser, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email:        "eval-lisbon@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Lisbon",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}
	lisbon := mustOnboard(t, pool, lisbonUser, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, lisbon.ID, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC))

	utcN, err := svc.Evaluate(ctx, utc)
	if err != nil {
		t.Fatal(err)
	}
	if utcN != 1 {
		t.Fatalf("UTC user created = %d, want 1", utcN)
	}

	lisbonN, err := svc.Evaluate(ctx, lisbon)
	if err != nil {
		t.Fatal(err)
	}
	if lisbonN != 0 {
		t.Fatalf("Lisbon user created = %d, want 0 (local day is the 15th, last check-in the 13th)", lisbonN)
	}
}

func TestEvaluateDoesNotCreateForNotOnboarded(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	user := seedUser(t, pool, "eval-raw@north.test")

	n, err := evalService(pool, now).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("not-onboarded user created = %d", n)
	}
}
