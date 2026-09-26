package soreness_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/soreness"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestSetReplacesAndClearRemoves(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "sore@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "T", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := soreness.NewService(soreness.NewRepository(pool))

	if _, err := svc.Set(ctx, user, "wings", 2, ""); err == nil {
		t.Error("unknown region accepted")
	}
	if _, err := svc.Set(ctx, user, "quads", 4, ""); err == nil {
		t.Error("severity 4 accepted")
	}
	for _, sev := range []int{1, 3} {
		if _, err := svc.Set(ctx, user, "quads", sev, ""); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := svc.OnDate(ctx, user, time.Now())
	if len(list) != 1 || list[0].Severity != 3 {
		t.Fatalf("today = %+v, want one region at 3", list)
	}
	if err := svc.Clear(ctx, user, "quads"); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.OnDate(ctx, user, time.Now()); len(list) != 0 {
		t.Errorf("cleared region still there: %+v", list)
	}
}
