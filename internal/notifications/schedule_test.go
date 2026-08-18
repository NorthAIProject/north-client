package notifications_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestValidateScheduleAcceptsTwoWeeksAndAReminder(t *testing.T) {
	in, err := notifications.ValidateSchedule(notifications.ScheduleInput{
		Kind: notifications.KindPhoto, Enabled: true, EveryDays: 14, ReminderDays: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.EveryDays != 14 || in.ReminderDays != 2 {
		t.Fatalf("%+v", in)
	}
}

func TestValidateScheduleRejectsAStrangeInterval(t *testing.T) {
	_, err := notifications.ValidateSchedule(notifications.ScheduleInput{
		Kind: notifications.KindPhoto, EveryDays: 10, ReminderDays: 0,
	})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("got %v", err)
	}
}

func TestUpsertAndReadPhotoSchedule(t *testing.T) {
	pool := testdb.New(t)
	svc := notifications.NewService(notifications.NewRepository(pool))
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "alerts@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Ada",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.PhotoSchedule(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.EveryDays != 14 {
		t.Fatalf("default = %+v", got)
	}

	saved, err := svc.UpsertSchedule(context.Background(), user.ID, notifications.ScheduleInput{
		Kind: notifications.KindPhoto, Enabled: true, EveryDays: 21, ReminderDays: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.EveryDays != 21 || saved.ReminderDays != 3 {
		t.Fatalf("saved = %+v", saved)
	}

	again, err := svc.PhotoSchedule(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.EveryDays != 21 {
		t.Fatalf("reread = %+v", again)
	}
	if !strings.Contains(again.Line(), "21") {
		t.Fatalf("line = %q", again.Line())
	}
}
