package decisions_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/decisions"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	userSvc := users.NewService(users.NewRepository(pool))
	u, err := userSvc.Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return u
}

func TestCreateAndListNewestFirst(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "list@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	first, err := svc.Create(ctx, user.ID, decisions.Input{Title: "Keep the day job"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(ctx, user.ID, decisions.Input{Title: "Move cities"})
	if err != nil {
		t.Fatal(err)
	}

	list, err := svc.List(ctx, user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2, got %d", len(list))
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("want newest first, got %q then %q", list[0].Title, list[1].Title)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "owner@north.test")
	stranger := seedUser(t, pool, "stranger@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	created, err := svc.Create(ctx, owner.ID, decisions.Input{Title: "Quit the evening client"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.Get(ctx, created.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger get: %v", err)
	}
	if _, err = svc.Update(ctx, created.ID, stranger.ID, decisions.Input{Title: "Hijack"}); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger update: %v", err)
	}
	if err = svc.Delete(ctx, created.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger delete: %v", err)
	}
	list, err := svc.List(ctx, stranger.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("stranger list = %d, want 0", len(list))
	}
}

func TestValidateRejectsEmptyTitle(t *testing.T) {
	_, err := decisions.Validate(decisions.Input{Title: "   "})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("empty title: %v", err)
	}
	var fields apperr.FieldErrors
	if !apperr.As(err, &fields) || fields.Messages()["title"] == "" {
		t.Fatalf("want a title field error, got %v", err)
	}
}

func TestValidateBoundsLongFields(t *testing.T) {
	longTitle := strings.Repeat("x", 201)
	if _, err := decisions.Validate(decisions.Input{Title: longTitle}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("long title: %v", err)
	}

	long := strings.Repeat("y", 2001)
	if _, err := decisions.Validate(decisions.Input{Title: "ok", Options: long}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("long options: %v", err)
	}
	if _, err := decisions.Validate(decisions.Input{Title: "ok", Rationale: long}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("long rationale: %v", err)
	}
	if _, err := decisions.Validate(decisions.Input{Title: "ok", Outcome: long}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("long outcome: %v", err)
	}
}

func TestUpdateRewritesFields(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "update@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	created, err := svc.Create(ctx, user.ID, decisions.Input{
		Title:     "Take the contract",
		Options:   "yes / no",
		Rationale: "money",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Update(ctx, created.ID, user.ID, decisions.Input{
		Title:     "Turn down the contract",
		Options:   "yes / no / later",
		Rationale: "energy",
		Outcome:   "slept better that week",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID {
		t.Fatalf("update changed id: %s vs %s", updated.ID, created.ID)
	}
	if updated.Title != "Turn down the contract" || updated.Options != "yes / no / later" ||
		updated.Rationale != "energy" || updated.Outcome != "slept better that week" {
		t.Fatalf("update did not rewrite: %+v", updated)
	}
}

func TestDeleteRemovesRow(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "delete@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	created, err := svc.Create(ctx, user.ID, decisions.Input{Title: "A call to undo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, created.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, created.ID, user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
}

func TestForContextFallsBackToRecentWhenQueryEmpty(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "recent@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	if _, err := svc.Create(ctx, user.ID, decisions.Input{Title: "Older call"}); err != nil {
		t.Fatal(err)
	}
	newer, err := svc.Create(ctx, user.ID, decisions.Input{Title: "Newer call"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.ForContext(ctx, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want both recent rows, got %d", len(got))
	}
	if got[0].ID != newer.ID {
		t.Fatalf("empty query should return newest first, got %q", got[0].Title)
	}
}

func TestForContextRanksKeywordMatchesFirst(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "rank@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	match, err := svc.Create(ctx, user.ID, decisions.Input{
		Title:     "Quit the evening client",
		Options:   "keep / quit / pause",
		Rationale: "the nights were costing more than they paid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Create(ctx, user.ID, decisions.Input{Title: "Bought a new bike"}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ForContext(ctx, user.ID, "thinking about quitting that client")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("want at least the matching decision")
	}
	if got[0].ID != match.ID {
		t.Fatalf("keyword match should rank above a newer unrelated row, got %q", got[0].Title)
	}
}

func TestForContextFallsBackWhenNothingMatches(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "nomatch@north.test")
	svc := decisions.NewService(decisions.NewRepository(pool))

	newer, err := svc.Create(ctx, user.ID, decisions.Input{Title: "Bought a new bike"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.ForContext(ctx, user.ID, "xylophone orchestra")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != newer.ID {
		t.Fatalf("unmatched query should fall back to newest, got %+v", got)
	}
}
