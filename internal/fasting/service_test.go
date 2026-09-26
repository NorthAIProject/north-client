package fasting_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/fasting"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestOneOpenFastAtATime(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "fast@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "T", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := fasting.NewService(fasting.NewRepository(pool))

	f, err := svc.Start(ctx, user, 0, nil)
	if err != nil || f.TargetHours != fasting.DefaultTargetHours || !f.Open() {
		t.Fatalf("start = %+v, %v", f, err)
	}
	if _, err := svc.Start(ctx, user, 16, nil); !apperr.Is(err, apperr.ErrConflict) {
		t.Errorf("second open fast = %v, want conflict", err)
	}
	if _, err := svc.Stop(ctx, user); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := svc.Current(ctx, user); ok {
		t.Error("stopped fast is still current")
	}
	if _, err := svc.Stop(ctx, user); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("stopping nothing = %v, want not found", err)
	}
	list, err := svc.Overlapping(ctx, user, timerange.Parse(timerange.KeyToday, user.Location()))
	if err != nil || len(list) != 1 {
		t.Errorf("overlapping = %d, %v", len(list), err)
	}
}
