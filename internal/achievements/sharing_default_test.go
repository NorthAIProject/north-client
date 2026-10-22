package achievements_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestSharingOnDefaults(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	a, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "sharing-default@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Ada", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	on := func(category string) bool {
		t.Helper()
		var v bool
		if err := pool.QueryRow(ctx, `SELECT sharing_on($1, $2)`, a.ID, category).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for c, want := range map[string]bool{"training": true, "streaks": true, "goals": true, "xp": false, "awards": true} {
		if got := on(c); got != want {
			t.Errorf("sharing_on(%s) = %v, want %v", c, got, want)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO achievement_sharing (user_id, category, shared) VALUES ($1, 'training', false)`, a.ID); err != nil {
		t.Fatal(err)
	}
	if on("training") {
		t.Error("stored false row must beat the default")
	}
}

func feedHas(t *testing.T, svc *achievements.Service, viewer uuid.UUID, title string) bool {
	t.Helper()
	items, err := svc.Feed(context.Background(), viewer, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Title == title {
			return true
		}
	}
	return false
}

func TestNewPersonSharesByDefault(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	svc := achievements.NewService(pool)
	soc := social.NewService(social.NewRepository(pool))
	a := person(t, pool, "default-a@north.test", "Ada")
	b := person(t, pool, "default-b@north.test", "Bo")
	follow(t, soc, a, b, "bo_default")

	got, err := svc.Sharing(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := (achievements.Sharing{Training: true, Streaks: true, Goals: true, XP: false, Awards: true}); got != want {
		t.Fatalf("Sharing(B) = %+v, want %+v", got, want)
	}
	svc.WorkoutCompleted(ctx, b.ID, uuid.New(), "Running", 30, time.Now().Add(-time.Hour))
	if !feedHas(t, svc, a.ID, "Finished Running") {
		t.Fatal("A should see B's workout with no switch touched")
	}
	if _, err := svc.SetSharing(ctx, b.ID, achievements.Sharing{}); err != nil {
		t.Fatal(err)
	}
	if feedHas(t, svc, a.ID, "Finished Running") {
		t.Fatal("A still sees B's workout after B switched everything off")
	}
}

func TestPinnedOffStaysOff(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	svc := achievements.NewService(pool)
	soc := social.NewService(social.NewRepository(pool))
	a := person(t, pool, "pinned-a@north.test", "Ada")
	b := person(t, pool, "pinned-b@north.test", "Bo")
	follow(t, soc, a, b, "bo_pinned")
	for _, c := range []string{"training", "streaks", "goals"} {
		if _, err := pool.Exec(ctx, `INSERT INTO achievement_sharing (user_id, category, shared) VALUES ($1, $2, false)`, b.ID, c); err != nil {
			t.Fatal(err)
		}
	}
	svc.WorkoutCompleted(ctx, b.ID, uuid.New(), "Running", 30, time.Now().Add(-time.Hour))
	if feedHas(t, svc, a.ID, "Finished Running") {
		t.Fatal("pinned-off B must stay out of A's feed")
	}
	got, err := svc.Sharing(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := (achievements.Sharing{Awards: true}); got != want {
		t.Fatalf("Sharing(B) = %+v, want only awards on", got)
	}
}

func TestSharingDefaultsAwardsOn(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	svc := achievements.NewService(pool)
	fresh := person(t, pool, "awards-fresh@north.test", "Fay")
	pinned := person(t, pool, "awards-pinned@north.test", "Pip")
	for _, c := range []string{"training", "streaks", "goals"} {
		if _, err := pool.Exec(ctx, `INSERT INTO achievement_sharing (user_id, category, shared) VALUES ($1, $2, false)`, pinned.ID, c); err != nil {
			t.Fatal(err)
		}
	}
	for name, id := range map[string]uuid.UUID{"fresh": fresh.ID, "pinned": pinned.ID} {
		got, err := svc.Sharing(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Awards {
			t.Errorf("%s user: Awards = false, want true", name)
		}
		var on bool
		if err := pool.QueryRow(ctx, `SELECT sharing_on($1, 'awards')`, id).Scan(&on); err != nil || !on {
			t.Errorf("%s user: sharing_on(awards) = %v, %v", name, on, err)
		}
	}
}
