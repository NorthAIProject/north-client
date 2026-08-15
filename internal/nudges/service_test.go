package nudges_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

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
	svc := nudges.NewService(nudges.NewRepository(pool))

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
	svc := nudges.NewService(nudges.NewRepository(pool))

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
	svc := nudges.NewService(nudges.NewRepository(pool))

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
	svc := nudges.NewService(nudges.NewRepository(pool))

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
