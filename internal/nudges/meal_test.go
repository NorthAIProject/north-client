package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

type stubMeals struct {
	reminders []meals.Reminder
	loggedAt  []time.Time
}

func (s *stubMeals) MealReminders(context.Context, uuid.UUID) ([]meals.Reminder, error) {
	return s.reminders, nil
}

func (s *stubMeals) LoggedFoodSince(_ context.Context, _ uuid.UUID, since time.Time) (bool, error) {
	for _, at := range s.loggedAt {
		if !at.Before(since) {
			return true, nil
		}
	}
	return false, nil
}

func lunchReminder() meals.Reminder {
	return meals.Reminder{
		ID: uuid.New(), Label: "log lunch", TimeOfDay: "13:00",
		DaysOfWeek: []int{0, 1, 2, 3, 4, 5, 6}, Enabled: true,
	}
}

func openMealReminders(t *testing.T, svc *nudges.Service, userID uuid.UUID) []nudges.Nudge {
	t.Helper()
	list, err := svc.ListOpen(context.Background(), userID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []nudges.Nudge
	for _, n := range list {
		if n.Kind == nudges.KindMealReminder {
			out = append(out, n)
		}
	}
	return out
}

func TestMealReminderRaisedOnceItsTimeComes(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := mustOnboard(t, pool, seedUser(t, pool, "meal-due@north.test"), time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	stub := &stubMeals{reminders: []meals.Reminder{lunchReminder()}}
	chat := &taggedFanoutSpy{}

	before := evalService(pool, time.Date(2026, 9, 2, 12, 30, 0, 0, time.UTC)).WithMeals(stub).WithFanout(chat)
	if _, err := before.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := openMealReminders(t, before, user.ID); len(got) != 0 {
		t.Fatalf("raised before 13:00: %+v", got)
	}

	after := evalService(pool, time.Date(2026, 9, 2, 13, 5, 0, 0, time.UTC)).WithMeals(stub).WithFanout(chat)
	for range 2 { // a second sweep must not send it again
		if _, err := after.Evaluate(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	got := openMealReminders(t, after, user.ID)
	if len(got) != 1 || got[0].Title != "log lunch" || got[0].DedupeKey != "2026-09-02:"+stub.reminders[0].ID.String() {
		t.Fatalf("open = %+v", got)
	}
	sent := 0
	for _, c := range chat.tagged {
		if c.kind == nudges.KindMealReminder {
			sent++
		}
	}
	if sent != 1 {
		t.Fatalf("sent to chat %d times, want 1", sent)
	}
}

func TestMealReminderSkippedWhenFoodWasJustLogged(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := mustOnboard(t, pool, seedUser(t, pool, "meal-logged@north.test"), time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	stub := &stubMeals{
		reminders: []meals.Reminder{lunchReminder()},
		loggedAt:  []time.Time{time.Date(2026, 9, 2, 12, 40, 0, 0, time.UTC)},
	}

	svc := evalService(pool, time.Date(2026, 9, 2, 13, 5, 0, 0, time.UTC)).WithMeals(stub)
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := openMealReminders(t, svc, user.ID); len(got) != 0 {
		t.Fatalf("raised though lunch was logged at 12:40: %+v", got)
	}
}

func TestMealReminderNotSentLongAfterItsTime(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := mustOnboard(t, pool, seedUser(t, pool, "meal-late@north.test"), time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	stub := &stubMeals{reminders: []meals.Reminder{lunchReminder()}}

	svc := evalService(pool, time.Date(2026, 9, 2, 18, 0, 0, 0, time.UTC)).WithMeals(stub)
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := openMealReminders(t, svc, user.ID); len(got) != 0 {
		t.Fatalf("lunch reminder sent at 18:00: %+v", got)
	}
}
