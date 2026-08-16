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

func TestNewAccountStartsOnTheDefaultTone(t *testing.T) {
	pool := testdb.New(t)
	svc := users.NewService(users.NewRepository(pool))

	user, err := svc.Register(context.Background(), users.Registration{
		Email:       "tone-default@north.test",
		DisplayName: "Toneless",
		Timezone:    "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.CoachingTone != users.ToneDefault {
		t.Fatalf("tone = %q, want %q", user.CoachingTone, users.ToneDefault)
	}
}

func TestUpdateProfileStoresTone(t *testing.T) {
	pool := testdb.New(t)
	svc := users.NewService(users.NewRepository(pool))
	ctx := context.Background()

	user := mustRegister(t, svc, "tone-set@north.test")

	updated, err := svc.UpdateProfile(ctx, user.ID, users.Profile{
		DisplayName:  "Tone Setter",
		Timezone:     "Europe/Lisbon",
		CoachingTone: users.ToneToughLove,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.CoachingTone != users.ToneToughLove {
		t.Fatalf("tone = %q, want %q", updated.CoachingTone, users.ToneToughLove)
	}

	// An empty tone is a form that predates the field, not a request for
	// silence about it.
	updated, err = svc.UpdateProfile(ctx, user.ID, users.Profile{
		DisplayName: "Tone Setter", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("update without tone: %v", err)
	}
	if updated.CoachingTone != users.ToneDefault {
		t.Fatalf("tone = %q, want the default", updated.CoachingTone)
	}
}

func TestUpdateProfileRefusesAnUnknownTone(t *testing.T) {
	pool := testdb.New(t)
	svc := users.NewService(users.NewRepository(pool))

	user := mustRegister(t, svc, "tone-bad@north.test")

	_, err := svc.UpdateProfile(context.Background(), user.ID, users.Profile{
		DisplayName: "Tone Setter", Timezone: "UTC", CoachingTone: users.Tone("shouty"),
	})

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field errors, got %v", err)
	}
	if fieldErrs.Messages()["coaching_tone"] == "" {
		t.Fatalf("no coaching_tone message: %v", fieldErrs.Messages())
	}
}

// The opposite of the tone rule, and deliberately so: a zone this build cannot
// resolve arrives from a browser or an import, and must never block a save.
func TestUpdateProfileFallsBackToUTC(t *testing.T) {
	pool := testdb.New(t)
	svc := users.NewService(users.NewRepository(pool))

	user := mustRegister(t, svc, "tz-fallback@north.test")

	updated, err := svc.UpdateProfile(context.Background(), user.ID, users.Profile{
		DisplayName: "Traveller", Timezone: "Mars/Olympus_Mons",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Timezone != "UTC" {
		t.Fatalf("timezone = %q, want UTC", updated.Timezone)
	}
}

func mustRegister(t *testing.T, svc *users.Service, email string) users.User {
	t.Helper()
	user, err := svc.Register(context.Background(), users.Registration{
		Email: email, DisplayName: "Test", Timezone: "UTC",
	})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return user
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
