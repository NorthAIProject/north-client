package profiles_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/profiles"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/xp"
)

type bodyweight struct{}

func (bodyweight) Current(context.Context, uuid.UUID) (biometrics.Biometric, error) {
	return biometrics.Biometric{WeightKg: 80, HeightCm: 180, Sex: biometrics.SexMale}, nil
}

type world struct {
	svc  *profiles.Service
	soc  *social.Service
	ach  *achievements.Service
	acts *activity.Service
	pool *pgxpool.Pool
}

func newWorld(t *testing.T) world {
	t.Helper()
	pool := testdb.New(t)
	acts := activity.NewService(activity.NewRepository(pool), bodyweight{})
	liftSvc := lifts.NewService(lifts.NewRepository(pool), nil).WithRecaps(acts, nil)
	soc := social.NewService(social.NewRepository(pool))
	ach := achievements.NewService(pool)
	xpSvc := xp.NewService(pool, checkins.NewService(checkins.NewRepository(pool), goals.NewService(goals.NewRepository(pool))))
	return world{svc: profiles.NewService(soc, ach, xpSvc, acts, liftSvc), soc: soc, ach: ach, acts: acts, pool: pool}
}

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

func (w world) handle(t *testing.T, u users.User, h string) {
	t.Helper()
	if _, err := w.soc.SetHandle(context.Background(), u.ID, h); err != nil {
		t.Fatal(err)
	}
}

func (w world) share(t *testing.T, u users.User, s achievements.Sharing) {
	t.Helper()
	if _, err := w.ach.SetSharing(context.Background(), u.ID, s); err != nil {
		t.Fatal(err)
	}
}

// workout finishes a session that ended `ago` before now.
func (w world) workout(t *testing.T, u users.User, ago time.Duration) activity.Session {
	t.Helper()
	se, err := w.acts.Log(context.Background(), u.ID, activity.LogInput{
		ActivityCode: "strength_training_light", StartedAt: time.Now().UTC().Add(-ago - time.Hour).Truncate(time.Second), Duration: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return se
}

func isNotFound(err error) bool { return errors.Is(err, apperr.ErrNotFound) }

var now = time.Now().UTC()

func TestProfileShowsWhatTheOwnerShares(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, ana := person(t, w.pool, "leo-ps@n.test", "Leo"), person(t, w.pool, "ana-ps@n.test", "Ana")
	w.handle(t, leo, "leo")
	w.workout(t, leo, 5*time.Hour)
	w.workout(t, leo, 30*time.Hour)

	v, err := w.svc.View(ctx, ana, "leo", now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Workouts == nil || *v.Workouts != 2 || len(v.Recent) != 2 || v.Level != nil {
		t.Fatalf("view = %+v", v)
	}
	if !v.Shares.Training || v.Shares.XP {
		t.Fatalf("shares = %+v", v.Shares)
	}
	if v.Recent[0].EndedAt.Before(v.Recent[1].EndedAt) {
		t.Fatal("recent is not newest first")
	}

	w.share(t, leo, achievements.Sharing{Training: true, XP: true})
	v, err = w.svc.View(ctx, ana, "leo", now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Level == nil || !v.Shares.XP {
		t.Fatalf("level not shown once xp is on: %+v", v)
	}
}

func TestPinnedOffOwnerShowsNoWorkouts(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, ana := person(t, w.pool, "leo-po@n.test", "Leo"), person(t, w.pool, "ana-po@n.test", "Ana")
	w.handle(t, leo, "leo")
	se := w.workout(t, leo, time.Hour)
	w.share(t, leo, achievements.Sharing{Training: false})

	v, err := w.svc.View(ctx, ana, "leo", now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Workouts != nil || v.Recent != nil || v.Shares.Training {
		t.Fatalf("view leaks workouts: %+v", v)
	}
	page, err := w.svc.Workouts(ctx, ana, "leo", time.Time{})
	if err != nil || page.Shared || len(page.Items) != 0 || page.Before != nil {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if _, err := w.svc.Workout(ctx, ana, "leo", se.ID); !isNotFound(err) {
		t.Fatalf("detail err = %v", err)
	}
}

func TestOwnProfileShowsEverything(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo := person(t, w.pool, "leo-own@n.test", "Leo")
	w.handle(t, leo, "leo")
	se := w.workout(t, leo, time.Hour)
	w.share(t, leo, achievements.Sharing{})

	v, err := w.svc.View(ctx, leo, "leo", now)
	if err != nil {
		t.Fatal(err)
	}
	if v.Workouts == nil || *v.Workouts != 1 || len(v.Recent) != 1 || v.Level == nil || v.Shares != (profiles.Shares{Training: true, XP: true}) {
		t.Fatalf("view = %+v", v)
	}
	page, err := w.svc.Workouts(ctx, leo, "leo", time.Time{})
	if err != nil || !page.Shared || len(page.Items) != 1 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if c, err := w.svc.Workout(ctx, leo, "leo", se.ID); err != nil || c.ID != se.ID {
		t.Fatalf("detail = %+v, %v", c, err)
	}
}

func TestBlockedPeopleGetNotFound(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, ana := person(t, w.pool, "leo-bl@n.test", "Leo"), person(t, w.pool, "ana-bl@n.test", "Ana")
	w.handle(t, leo, "leo")
	w.handle(t, ana, "ana")
	leoSe, anaSe := w.workout(t, leo, time.Hour), w.workout(t, ana, time.Hour)

	check := func(label string) {
		t.Helper()
		if _, err := w.svc.View(ctx, ana, "leo", now); !isNotFound(err) {
			t.Fatalf("%s: View err = %v", label, err)
		}
		if _, err := w.svc.Workouts(ctx, ana, "leo", time.Time{}); !isNotFound(err) {
			t.Fatalf("%s: Workouts err = %v", label, err)
		}
		if _, err := w.svc.Workout(ctx, ana, "leo", leoSe.ID); !isNotFound(err) {
			t.Fatalf("%s: Workout err = %v", label, err)
		}
	}
	if err := w.soc.Block(ctx, leo.ID, ana.ID); err != nil {
		t.Fatal(err)
	}
	check("owner blocked viewer")
	if err := w.soc.Unblock(ctx, leo.ID, ana.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.soc.Block(ctx, ana.ID, leo.ID); err != nil {
		t.Fatal(err)
	}
	check("viewer blocked owner")
	_ = anaSe
}

func TestWorkoutMustBelongToTheHandle(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, zoe, ana := person(t, w.pool, "leo-mb@n.test", "Leo"), person(t, w.pool, "zoe-mb@n.test", "Zoe"), person(t, w.pool, "ana-mb@n.test", "Ana")
	w.handle(t, leo, "leo")
	w.handle(t, zoe, "zoe")
	zoeSe := w.workout(t, zoe, time.Hour)
	if _, err := w.svc.Workout(ctx, ana, "leo", zoeSe.ID); !isNotFound(err) {
		t.Fatalf("someone else's session via leo's handle: %v", err)
	}
	if c, err := w.svc.Workout(ctx, ana, "zoe", zoeSe.ID); err != nil || c.UserID != zoe.ID {
		t.Fatalf("own handle = %+v, %v", c, err)
	}

	cancelled, err := w.acts.Start(ctx, leo.ID, "strength_training_light", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.acts.Cancel(ctx, cancelled.ID, leo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Workout(ctx, ana, "leo", cancelled.ID); !isNotFound(err) {
		t.Fatalf("cancelled session: %v", err)
	}
	if _, err := w.svc.Workout(ctx, ana, "leo", uuid.New()); !isNotFound(err) {
		t.Fatalf("unknown session: %v", err)
	}
}

func TestWorkoutsPageByEndedAt(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	leo, ana := person(t, w.pool, "leo-pg@n.test", "Leo"), person(t, w.pool, "ana-pg@n.test", "Ana")
	w.handle(t, leo, "leo")
	for i := range 23 {
		w.workout(t, leo, time.Duration(i+1)*2*time.Hour)
	}
	first, err := w.svc.Workouts(ctx, ana, "leo", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 20 || first.Before == nil || !first.Shared {
		t.Fatalf("first = %d items, before %v", len(first.Items), first.Before)
	}
	if !first.Before.Equal(first.Items[19].EndedAt) {
		t.Fatal("before is not the last item's end")
	}
	for i := 1; i < len(first.Items); i++ {
		if first.Items[i].EndedAt.After(first.Items[i-1].EndedAt) {
			t.Fatal(fmt.Sprint("not newest first at ", i))
		}
	}
	second, err := w.svc.Workouts(ctx, ana, "leo", *first.Before)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 3 || second.Before != nil {
		t.Fatalf("second = %d items, before %v", len(second.Items), second.Before)
	}
}
