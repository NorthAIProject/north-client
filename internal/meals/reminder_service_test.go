package meals_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestValidateReminderDefaultsDaysAndRejectsBadTime(t *testing.T) {
	t.Parallel()

	clean, err := meals.ValidateReminder(meals.ReminderInput{Label: "Log lunch", TimeOfDay: "12:30"})
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if len(clean.DaysOfWeek) != 7 {
		t.Fatalf("expected all 7 days to default, got %d", len(clean.DaysOfWeek))
	}

	if _, err := meals.ValidateReminder(meals.ReminderInput{Label: "x", TimeOfDay: "25:99"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation for a bad time, got %v", err)
	}
	if _, err := meals.ValidateReminder(meals.ReminderInput{TimeOfDay: "12:00"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation for a missing label, got %v", err)
	}
}

func TestCreateListUpdateDeleteReminder(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	svc := meals.NewMealReminderService(meals.NewRepository(pool))
	ctx := context.Background()

	created, err := svc.Create(ctx, user.ID, meals.ReminderInput{Label: "Log lunch", TimeOfDay: "12:30"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := svc.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 reminder, got %d", len(list))
	}

	updated, err := svc.Update(ctx, created.ID, user.ID, meals.ReminderInput{Label: "Log dinner", TimeOfDay: "19:00"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Label != "Log dinner" {
		t.Fatalf("label = %q", updated.Label)
	}

	toggled, err := svc.Toggle(ctx, created.ID, user.ID, false)
	if err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if toggled.Enabled {
		t.Fatal("expected the reminder to be disabled")
	}

	if err := svc.Delete(ctx, created.ID, user.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err = svc.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no reminders left, got %d", len(list))
	}
}

func TestDueNowFiresOnceAndRespectsScheduling(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	svc := meals.NewMealReminderService(meals.NewRepository(pool))
	ctx := context.Background()

	now := time.Now()
	todayWeekday := int(now.Weekday())
	otherWeekday := (todayWeekday + 1) % 7

	past := now.Add(-time.Hour).Format("15:04")

	dueToday, err := svc.Create(ctx, user.ID, meals.ReminderInput{
		Label: "Due today", TimeOfDay: past, DaysOfWeek: []int{todayWeekday},
	})
	if err != nil {
		t.Fatalf("create due reminder: %v", err)
	}
	if _, err := svc.Create(ctx, user.ID, meals.ReminderInput{
		Label: "Not today", TimeOfDay: past, DaysOfWeek: []int{otherWeekday},
	}); err != nil {
		t.Fatalf("create not-today reminder: %v", err)
	}
	future := now.Add(time.Hour).Format("15:04")
	if _, err := svc.Create(ctx, user.ID, meals.ReminderInput{
		Label: "Later today", TimeOfDay: future, DaysOfWeek: []int{todayWeekday},
	}); err != nil {
		t.Fatalf("create later-today reminder: %v", err)
	}

	due, err := svc.DueNow(ctx, user.ID, now)
	if err != nil {
		t.Fatalf("due now: %v", err)
	}
	if len(due) != 1 || due[0].ID != dueToday.ID {
		t.Fatalf("expected only %q due, got %d reminders", dueToday.Label, len(due))
	}

	// Calling again the same day should not return it a second time.
	again, err := svc.DueNow(ctx, user.ID, now)
	if err != nil {
		t.Fatalf("due now (again): %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected the reminder not to fire twice in one day, got %d", len(again))
	}
}
