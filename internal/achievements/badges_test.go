package achievements_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social"
)

func badgeFeed(t *testing.T, ach *achievements.Service, viewer uuid.UUID) []achievements.Item {
	t.Helper()
	got, err := ach.Feed(context.Background(), viewer, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPrivateBadgeNeverLeavesOwner(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ach := achievements.NewService(pool)
	aw := awards.NewService(pool)
	soc := social.NewService(social.NewRepository(pool))
	ana, leo := person(t, pool, "pb-ana@north.test", "Ana"), person(t, pool, "pb-leo@north.test", "Leo")
	follow(t, soc, leo, ana, "pbana")
	at := time.Now().Add(-time.Hour)
	aw.Grant(ctx, ana.ID, at, "water_week") // hydration: private
	aw.Grant(ctx, ana.ID, at.Add(-time.Hour), "workouts_1")

	if got := badgeFeed(t, ach, leo.ID); len(got) != 1 || got[0].BadgeID != "workouts_1" {
		t.Fatalf("follower feed = %+v, want only the public badge", got)
	}
	own := badgeFeed(t, ach, ana.ID)
	if len(own) != 2 {
		t.Fatalf("own feed = %d items, want both", len(own))
	}
	var priv uuid.UUID
	for _, it := range own {
		if it.BadgeID == "water_week" {
			priv = it.ID
			if !it.Private {
				t.Fatal("private flag not carried")
			}
		}
	}
	if err := ach.GiveKudos(ctx, leo.ID, priv, "Leo"); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("kudos on a private badge = %v, want not found", err)
	}
	hl, err := ach.OwnBetween(ctx, ana.ID, at.Add(-3*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(hl) != 1 || hl[0].Title == "" {
		t.Fatalf("recap highlights = %+v, want only the public badge", hl)
	}
}

func TestOneCardPerMoment(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ach := achievements.NewService(pool)
	aw := awards.NewService(pool)
	ana := person(t, pool, "om-ana@north.test", "Ana")
	at := time.Now().Add(-time.Hour)
	for _, id := range []string{"workouts_1", "pr_1", "century"} {
		aw.Grant(ctx, ana.ID, at, id)
	}
	aw.Grant(ctx, ana.ID, at.Add(-time.Hour), "workouts_10")
	got := badgeFeed(t, ach, ana.ID)
	if len(got) != 2 {
		t.Fatalf("items = %d, want 2", len(got))
	}
	if len(got[0].Badges) != 3 || got[0].Badges[0] != got[0].BadgeID {
		t.Fatalf("badges = %v first %s", got[0].Badges, got[0].BadgeID)
	}
	if got[0].Title == "" || got[0].Title == got[0].BadgeID {
		t.Fatalf("title = %q, want catalogue copy", got[0].Title)
	}
}

func TestStreakBadgeHidesStreakReached(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ach := achievements.NewService(pool)
	aw := awards.NewService(pool)
	ana := person(t, pool, "sb-ana@north.test", "Ana")
	at := time.Now().Add(-time.Minute)
	ach.StreakReached(ctx, ana.ID, 7, "2026-10-22", at)
	if got := badgeFeed(t, ach, ana.ID); len(got) != 1 {
		t.Fatalf("streak mark alone = %d items", len(got))
	}
	aw.Grant(ctx, ana.ID, at, "streak_7")
	got := badgeFeed(t, ach, ana.ID)
	if len(got) != 1 || got[0].BadgeID != "streak_7" {
		t.Fatalf("items = %+v, want only the badge", got)
	}
}

func TestAwardsSwitchOffHidesBadges(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ach := achievements.NewService(pool)
	aw := awards.NewService(pool)
	soc := social.NewService(social.NewRepository(pool))
	ana, leo := person(t, pool, "sw-ana@north.test", "Ana"), person(t, pool, "sw-leo@north.test", "Leo")
	follow(t, soc, leo, ana, "swana")
	aw.Grant(ctx, ana.ID, time.Now().Add(-time.Hour), "workouts_1")
	if len(badgeFeed(t, ach, leo.ID)) != 1 {
		t.Fatal("badge hidden while the switch is on")
	}
	if _, err := ach.SetSharing(ctx, ana.ID, achievements.Sharing{Training: true, Streaks: true, Goals: true}); err != nil {
		t.Fatal(err)
	}
	if got := badgeFeed(t, ach, leo.ID); len(got) != 0 {
		t.Fatalf("follower sees %+v with awards off", got)
	}
	if len(badgeFeed(t, ach, ana.ID)) != 1 {
		t.Fatal("owner lost own badge")
	}
}
