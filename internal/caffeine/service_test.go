package caffeine_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/caffeine"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestLogPresetAndUndo(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "coffee@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "T", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := caffeine.NewService(caffeine.NewRepository(pool))

	e, err := svc.Log(ctx, user, caffeine.LogInput{Preset: "coffee"})
	if err != nil || e.MG != 100 || e.Label != "coffee" {
		t.Fatalf("preset log = %+v, %v", e, err)
	}
	if _, err := svc.Log(ctx, user, caffeine.LogInput{MG: 5000}); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("5000mg should be refused, got %v", err)
	}
	if _, err := svc.Log(ctx, user, caffeine.LogInput{Preset: "absinthe"}); err == nil {
		t.Error("unknown preset accepted")
	}
	today, _ := svc.Today(ctx, user)
	if len(today) != 1 {
		t.Fatalf("today = %d entries", len(today))
	}
	if err := svc.Undo(ctx, user, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Undo(ctx, user, e.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("second undo = %v, want not found", err)
	}
}
