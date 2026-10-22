package awards_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/fasting"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

type recorder struct {
	users []uuid.UUID
	areas [][]award.Area
}

func (r *recorder) Check(_ context.Context, id uuid.UUID, _ time.Time, areas ...award.Area) {
	r.users = append(r.users, id)
	r.areas = append(r.areas, areas)
}

func (r *recorder) only(t *testing.T, user uuid.UUID, area award.Area) {
	t.Helper()
	if len(r.users) != 1 || r.users[0] != user || len(r.areas[0]) != 1 || r.areas[0][0] != area {
		t.Fatalf("checks = %v %v, want one %s check for the person", r.users, r.areas, area)
	}
}

func (r *recorder) none(t *testing.T) {
	t.Helper()
	if len(r.users) != 0 {
		t.Fatalf("checked after a failed write: %v %v", r.users, r.areas)
	}
}

func person(t *testing.T, e *env) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(e.pool)).ByID(context.Background(), e.user)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestHydrationHook(t *testing.T) {
	e := newEnv(t, "UTC")
	u, rec := person(t, e), &recorder{}
	svc := hydration.NewService(hydration.NewRepository(e.pool)).WithAwards(rec)
	if _, err := svc.Log(context.Background(), u, 0); err == nil {
		t.Fatal("zero drink accepted")
	}
	rec.none(t)
	if _, err := svc.Log(context.Background(), u, 250); err != nil {
		t.Fatal(err)
	}
	rec.only(t, u.ID, award.Hydration)
}

func TestMindHook(t *testing.T) {
	e := newEnv(t, "UTC")
	rec := &recorder{}
	svc := mind.NewService(mind.NewRepository(e.pool), nil).WithAwards(rec)
	if _, err := svc.Create(context.Background(), e.user, mind.Input{Content: ""}); err == nil {
		t.Fatal("empty entry accepted")
	}
	rec.none(t)
	if _, err := svc.Create(context.Background(), e.user, mind.Input{Content: "a good day"}); err != nil {
		t.Fatal(err)
	}
	rec.only(t, e.user, award.Mind)
}

func TestSleepHook(t *testing.T) {
	e := newEnv(t, "UTC")
	u, rec := person(t, e), &recorder{}
	svc := sleep.NewService(sleep.NewRepository(e.pool)).WithAwards(rec)
	if _, err := svc.LogToday(context.Background(), u, sleep.Input{DurationMinutes: 0}); err == nil {
		t.Fatal("empty night accepted")
	}
	rec.none(t)
	if _, err := svc.LogToday(context.Background(), u, sleep.Input{DurationMinutes: 450, Quality: func() *int { q := 4; return &q }()}); err != nil {
		t.Fatal(err)
	}
	rec.only(t, u.ID, award.Sleep)
}

func TestHabitHook(t *testing.T) {
	e := newEnv(t, "UTC")
	u, rec := person(t, e), &recorder{}
	svc := habits.NewService(habits.NewRepository(e.pool)).WithAwards(rec)
	if err := svc.Complete(context.Background(), u, uuid.New()); err == nil {
		t.Fatal("unknown habit completed")
	}
	rec.none(t)
	h, err := svc.Create(context.Background(), u, habits.Input{Name: "Stretch", Domain: "fitness", Days: []time.Weekday{0, 1, 2, 3, 4, 5, 6}, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Complete(context.Background(), u, h.ID); err != nil {
		t.Fatal(err)
	}
	rec.only(t, u.ID, award.Habits)
}

func TestFastingHook(t *testing.T) {
	e := newEnv(t, "UTC")
	u, rec := person(t, e), &recorder{}
	svc := fasting.NewService(fasting.NewRepository(e.pool)).WithAwards(rec)
	if _, err := svc.Stop(context.Background(), u); err == nil {
		t.Fatal("stopped a fast that was not open")
	}
	rec.none(t)
	if _, err := svc.Start(context.Background(), u, 16, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	rec.only(t, u.ID, award.Body)
}
