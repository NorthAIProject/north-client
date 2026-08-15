package checkins_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email, tz string) users.User {
	t.Helper()
	userSvc := users.NewService(users.NewRepository(pool))
	u, err := userSvc.Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     tz,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return u
}

func TestUpsertTodayIsOnePerDay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "fernando@north.test", "Europe/Lisbon")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	first, err := svc.UpsertToday(ctx, user, checkins.Input{Mood: 3, Energy: 3, Wins: "walked"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.UpsertToday(ctx, user, checkins.Input{Mood: 5, Energy: 4, Wins: "ran"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("same day should upsert, got %s vs %s", first.ID, second.ID)
	}
	if second.Mood != 5 || second.Wins != "ran" {
		t.Fatalf("upsert did not replace: %+v", second)
	}

	list, err := svc.List(ctx, user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 row, got %d", len(list))
	}
}

func TestOwnershipIsolation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "owner@north.test", "UTC")
	stranger := seedUser(t, pool, "stranger@north.test", "UTC")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	created, err := svc.UpsertToday(ctx, owner, checkins.Input{Mood: 4, Energy: 4})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.Get(ctx, created.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger get: %v", err)
	}
	list, err := svc.List(ctx, stranger.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("stranger list should be empty, got %d", len(list))
	}
}

func TestValidateMoodEnergy(t *testing.T) {
	t.Parallel()
	_, err := checkins.Validate(checkins.Input{Mood: 0, Energy: 3})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var fe apperr.FieldErrors
	if !apperr.As(err, &fe) || fe.Messages()["mood"] == "" {
		t.Fatalf("want mood field error, got %v", err)
	}
}

func TestStreak(t *testing.T) {
	// Offsets are calendar days from today in the user's zone: 0 is today, -1 is yesterday.
	type dayOffset int

	tests := []struct {
		name     string
		timezone string
		offsets  []dayOffset
		want     int
	}{
		{
			// A positive UTC offset rather than "UTC": converting local midnight
			// to UTC lands on the previous day, which zeroed the streak for every
			// such user while the only caller that pinned the behaviour stayed green.
			name:     "three consecutive days in Lisbon",
			timezone: "Europe/Lisbon",
			offsets:  []dayOffset{0, -1, -2},
			want:     3,
		},
		{
			name:     "yesterday only still counts (not checked in today)",
			timezone: "Europe/Lisbon",
			offsets:  []dayOffset{-1, -2},
			want:     2,
		},
		{
			name:     "gap breaks the streak",
			timezone: "Europe/Lisbon",
			offsets:  []dayOffset{0, -2},
			want:     1,
		},
		{
			name:     "empty history is zero not failure",
			timezone: "Europe/Lisbon",
			want:     0,
		},
		{
			name:     "last check-in older than yesterday is zero",
			timezone: "Europe/Lisbon",
			offsets:  []dayOffset{-3},
			want:     0,
		},
		{
			name:     "Auckland today is not UTC yesterday",
			timezone: "Pacific/Auckland",
			offsets:  []dayOffset{0},
			want:     1,
		},
		{
			name:     "invalid timezone falls back to UTC and still counts today",
			timezone: "Not/AZone",
			offsets:  []dayOffset{0},
			want:     1,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := testdb.New(t)
			ctx := context.Background()
			user := seedUser(t, pool, fmt.Sprintf("streak-%d@north.test", i), tt.timezone)
			repo := checkins.NewRepository(pool)
			svc := checkins.NewService(repo, nil)

			today := checkins.LocalDate(user, time.Now())
			for _, off := range tt.offsets {
				day := today.AddDate(0, 0, int(off))
				if _, err := repo.Upsert(ctx, user.ID, checkins.Write{
					LocalDate: day,
					Mood:      3,
					Energy:    3,
				}); err != nil {
					t.Fatal(err)
				}
			}

			got, err := svc.Streak(ctx, user)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Streak = %d, want %d (tz=%s loc=%s today=%s)",
					got, tt.want, tt.timezone, user.Location(), today.Format("2006-01-02"))
			}
		})
	}
}

func TestDeleteOwnershipIsolation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "deleteowner@north.test", "UTC")
	stranger := seedUser(t, pool, "deletestranger@north.test", "UTC")
	svc := checkins.NewService(checkins.NewRepository(pool), nil)

	created, err := svc.UpsertToday(ctx, owner, checkins.Input{Mood: 3, Energy: 3})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(ctx, created.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger delete: %v", err)
	}

	if err := svc.Delete(ctx, created.ID, owner.ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}

	if _, err := svc.Get(ctx, created.ID, owner.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestListPopulatesRelatedGoalTitle(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "goaltitle@north.test", "UTC")
	goalSvc := goals.NewService(goals.NewRepository(pool))
	svc := checkins.NewService(checkins.NewRepository(pool), goalSvc)

	g, err := goalSvc.Create(ctx, user.ID, goals.Input{
		Title:    "Run a marathon",
		Category: goals.CategoryFitness,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.UpsertToday(ctx, user, checkins.Input{
		Mood: 4, Energy: 4, RelatedGoalID: &g.ID,
	}); err != nil {
		t.Fatal(err)
	}

	list, err := svc.List(ctx, user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 row, got %d", len(list))
	}
	if list[0].RelatedGoalTitle != "Run a marathon" {
		t.Fatalf("RelatedGoalTitle = %q, want %q", list[0].RelatedGoalTitle, "Run a marathon")
	}
}

func TestRelatedGoalMustBelongToUser(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "goalowner@north.test", "UTC")
	stranger := seedUser(t, pool, "goalstranger@north.test", "UTC")
	goalSvc := goals.NewService(goals.NewRepository(pool))
	svc := checkins.NewService(checkins.NewRepository(pool), goalSvc)

	g, err := goalSvc.Create(ctx, stranger.ID, goals.Input{
		Title:    "Stranger goal",
		Category: goals.CategoryFitness,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.UpsertToday(ctx, user, checkins.Input{
		Mood: 3, Energy: 3, RelatedGoalID: &g.ID,
	})
	if err == nil {
		t.Fatal("expected ownership error for foreign goal")
	}
}

func TestRelatedGoalMustBeActive(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "activegoal@north.test", "UTC")
	goalSvc := goals.NewService(goals.NewRepository(pool))
	svc := checkins.NewService(checkins.NewRepository(pool), goalSvc)

	g, err := goalSvc.Create(ctx, user.ID, goals.Input{
		Title:    "Run a marathon",
		Category: goals.CategoryFitness,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = goalSvc.SetStatus(ctx, g.ID, user.ID, goals.StatusPaused); err != nil {
		t.Fatal(err)
	}

	_, err = svc.UpsertToday(ctx, user, checkins.Input{
		Mood: 3, Energy: 3, RelatedGoalID: &g.ID,
	})
	if err == nil {
		t.Fatal("expected rejection for inactive goal")
	}
	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field error, got %v", err)
	}
	if msg := fieldErrs.Messages()["related_goal_id"]; msg != "That goal is not available." {
		t.Fatalf("related_goal_id error = %q", msg)
	}
}

func TestUpdateRelatedGoalMustBelongToUser(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "updategoalowner@north.test", "UTC")
	stranger := seedUser(t, pool, "updategoalstranger@north.test", "UTC")
	goalSvc := goals.NewService(goals.NewRepository(pool))
	svc := checkins.NewService(checkins.NewRepository(pool), goalSvc)

	created, err := svc.UpsertToday(ctx, user, checkins.Input{Mood: 3, Energy: 3})
	if err != nil {
		t.Fatal(err)
	}

	g, err := goalSvc.Create(ctx, stranger.ID, goals.Input{
		Title:    "Stranger goal",
		Category: goals.CategoryFitness,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Update(ctx, created.ID, user.ID, checkins.Input{
		Mood: 3, Energy: 3, RelatedGoalID: &g.ID,
	})
	if err == nil {
		t.Fatal("expected ownership error for foreign goal")
	}
}

// seedDays writes one check-in per offset, counted back from the user's local
// today. Backdating cannot go through UpsertToday, which always writes today.
func seedDays(t *testing.T, repo *checkins.Repository, user users.User, offsets ...int) {
	t.Helper()
	today := checkins.LocalDate(user, time.Now())
	for _, offset := range offsets {
		if _, err := repo.Upsert(context.Background(), user.ID, checkins.Write{
			LocalDate: today.AddDate(0, 0, -offset),
			Mood:      3,
			Energy:    3,
		}); err != nil {
			t.Fatalf("seed day -%d: %v", offset, err)
		}
	}
}

// TestRecentForContextHonoursTheWindow pins the boundary the coach relies on:
// a check-in from three weeks ago is history, not context.
func TestRecentForContextHonoursTheWindow(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "window@north.test", "Europe/Lisbon")
	repo := checkins.NewRepository(pool)
	svc := checkins.NewService(repo, nil)

	seedDays(t, repo, user, 0, 3, 13, 20)

	list, err := svc.RecentForContext(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want the 3 check-ins inside the 14-day window, got %d", len(list))
	}

	today := checkins.LocalDate(user, time.Now())
	oldest := today.AddDate(0, 0, -20)
	for _, c := range list {
		if c.LocalDate.Equal(oldest) {
			t.Fatalf("the 20-day-old check-in should be outside the window: %+v", c)
		}
	}

	// Newest first: the coach reads the top of the list as "most recent".
	for i := 1; i < len(list); i++ {
		if !list[i-1].LocalDate.After(list[i].LocalDate) {
			t.Fatalf("check-ins should be newest first, got %s before %s",
				list[i-1].LocalDate, list[i].LocalDate)
		}
	}
}

// TestRecentForContextReturnsAFullFortnight guards the row cap against the
// window: a user who checks in every day must not silently lose the older half
// of the fortnight the coach is told it can see.
func TestRecentForContextReturnsAFullFortnight(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "fortnight@north.test", "Europe/Lisbon")
	repo := checkins.NewRepository(pool)
	svc := checkins.NewService(repo, nil)

	offsets := make([]int, 0, 14)
	for i := range 14 {
		offsets = append(offsets, i)
	}
	seedDays(t, repo, user, offsets...)

	list, err := svc.RecentForContext(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 14 {
		t.Fatalf("14 consecutive days of check-ins should all reach the coach, got %d", len(list))
	}
}
