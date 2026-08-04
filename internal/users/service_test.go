package users_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestRegisterWithoutPasswordStoresNullHash(t *testing.T) {
	pool := testdb.New(t)
	svc := users.NewService(users.NewRepository(pool))
	ctx := context.Background()

	user, err := svc.Register(ctx, users.Registration{
		Email:       "passkey@north.test",
		DisplayName: "Passkey User",
		Timezone:    "UTC",
		// Empty password hash → NULL column for passkey/OAuth accounts.
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	var hash *string
	if err = pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", user.ID).Scan(&hash); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hash != nil {
		t.Fatalf("expected null password_hash, got %q", *hash)
	}

	_, gotHash, err := svc.CredentialsByEmail(ctx, "passkey@north.test")
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	if gotHash != "" {
		t.Fatalf("CredentialsByEmail should return empty string for null hash, got %q", gotHash)
	}
}

func TestRegisterWithExplicitID(t *testing.T) {
	pool := testdb.New(t)
	svc := users.NewService(users.NewRepository(pool))
	ctx := context.Background()

	id := uuid.New()
	user, err := svc.Register(ctx, users.Registration{
		ID:          id,
		Email:       "with-id@north.test",
		DisplayName: "With ID",
		Timezone:    "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.ID != id {
		t.Fatalf("id = %s, want %s", user.ID, id)
	}
}

func TestRegisterConflict(t *testing.T) {
	pool := testdb.New(t)
	svc := users.NewService(users.NewRepository(pool))
	ctx := context.Background()

	reg := users.Registration{
		Email:        "dup@north.test",
		DisplayName:  "One",
		Timezone:     "UTC",
		PasswordHash: "$2a$12$notarealhashbutlongenoughfortestxx",
	}
	if _, err := svc.Register(ctx, reg); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := svc.Register(ctx, reg)
	if !apperr.Is(err, apperr.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}
