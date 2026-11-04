package aiconsent_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/aiconsent"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestConsentLifecycle(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email:        uuid.NewString() + "@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	userID := user.ID

	svc := aiconsent.NewService(pool)

	got, err := svc.Get(ctx, userID)
	if err != nil || got.Status != aiconsent.StatusUnset || got.DecidedAt != nil {
		t.Fatalf("never asked: got %+v, %v", got, err)
	}
	allowed, err := svc.AllowsAI(ctx, userID)
	if err != nil || allowed {
		t.Fatalf("never asked must refuse: %v, %v", allowed, err)
	}

	if _, err = svc.Set(ctx, userID, aiconsent.StatusGranted); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if ok, _ := svc.AllowsAI(ctx, userID); !ok {
		t.Fatal("granted must allow")
	}

	declined, err := svc.Set(ctx, userID, aiconsent.StatusDeclined)
	if err != nil || declined.Status != aiconsent.StatusDeclined || declined.Version != aiconsent.Version || declined.DecidedAt == nil {
		t.Fatalf("decline: %+v, %v", declined, err)
	}
	if ok, _ := svc.AllowsAI(ctx, userID); ok {
		t.Fatal("declined must refuse")
	}

	if _, err = svc.Set(ctx, userID, "maybe"); err == nil {
		t.Fatal("an unknown decision must be rejected")
	}
}
