package crews_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func person(t *testing.T, pool *pgxpool.Pool, email, name string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: name, Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func checkIn(t *testing.T, pool *pgxpool.Pool, userID users.User, day time.Time) {
	t.Helper()
	if _, err := checkins.NewRepository(pool).Upsert(context.Background(), userID.ID, checkins.Write{
		LocalDate: time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC), Mood: 4, Energy: 3,
	}); err != nil {
		t.Fatal(err)
	}
}

// Wednesday 1 October 2026, 19:00 UTC: the week started on Monday the 29th.
var wednesday = time.Date(2026, 9, 30, 19, 0, 0, 0, time.UTC).AddDate(0, 0, 1)

func service(pool *pgxpool.Pool) *crews.Service {
	cs := checkins.NewService(checkins.NewRepository(pool), nil)
	return crews.NewService(pool, crews.CheckInsFrom(cs), nil).WithClock(func() time.Time { return wednesday })
}

// The board: who checked in today, streaks, and the week's challenge, for
// every member, as any member sees it.
func TestBoard(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := service(pool)
	ana := person(t, pool, "ana@north.test", "Ana")
	leo := person(t, pool, "leo@north.test", "Leo")

	c, err := svc.Create(ctx, ana.ID, "  Morning runners ")
	if err != nil || c.Name != "Morning runners" {
		t.Fatalf("create = %+v, %v", c, err)
	}
	if _, err = svc.Join(ctx, leo.ID, c.Code); err != nil {
		t.Fatal(err)
	}
	if err = svc.SetChallenge(ctx, c.ID, ana.ID, &crews.Challenge{Kind: crew.ChallengeCheckIns, Target: 5}); err != nil {
		t.Fatal(err)
	}
	if err = svc.SetChallenge(ctx, c.ID, leo.ID, &crews.Challenge{Kind: crew.ChallengeCheckIns, Target: 3}); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a member set the challenge: %v", err)
	}

	// Ana: Monday to today. Leo: only last week.
	for back := 0; back <= 2; back++ {
		checkIn(t, pool, ana, wednesday.AddDate(0, 0, -back))
	}
	checkIn(t, pool, leo, wednesday.AddDate(0, 0, -7))

	board, err := svc.Board(ctx, c.ID, leo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if board.Challenge == nil || board.Challenge.Target != 5 || board.IsOwner {
		t.Fatalf("board = %+v", board)
	}
	got := map[string]crews.Member{}
	for _, m := range board.Members {
		got[m.DisplayName] = m
	}
	if a := got["Ana"]; !a.CheckedIn || a.Streak != 3 || a.WeekProgress != 3 || !a.Owner {
		t.Fatalf("ana = %+v", a)
	}
	if l := got["Leo"]; l.CheckedIn || l.WeekProgress != 0 || !l.Me {
		t.Fatalf("leo = %+v", l)
	}

	// Outsiders see nothing.
	sam := person(t, pool, "sam@north.test", "Sam")
	if _, err = svc.Board(ctx, c.ID, sam.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("outsider read the board: %v", err)
	}
}

// Eight is full for a free owner; the owner leaving hands the crew on; the
// last one out deletes it.
func TestMembership(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := service(pool)
	owner := person(t, pool, "owner@north.test", "Owner")
	c, _ := svc.Create(ctx, owner.ID, "Eight")

	var first users.User
	for i := 1; i < crew.MaxMembers; i++ {
		u := person(t, pool, fmt.Sprintf("m%d@north.test", i), fmt.Sprintf("M%d", i))
		if i == 1 {
			first = u
		}
		if _, err := svc.Join(ctx, u.ID, c.Code); err != nil {
			t.Fatalf("member %d: %v", i, err)
		}
	}
	late := person(t, pool, "late@north.test", "Late")
	if _, err := svc.Join(ctx, late.ID, c.Code); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("ninth member joined: %v", err)
	}

	if err := svc.Leave(ctx, c.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	board, err := svc.Board(ctx, c.ID, first.ID)
	if err != nil || board.OwnerID != first.ID || len(board.Members) != crew.MaxMembers-1 {
		t.Fatalf("after owner left: owner %v, %d members, %v", board.OwnerID, len(board.Members), err)
	}

	solo, _ := svc.Create(ctx, late.ID, "Solo")
	if err := svc.Leave(ctx, solo.ID, late.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Preview(ctx, solo.Code); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("empty crew still exists: %v", err)
	}
}
