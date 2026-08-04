package auth_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestFindOrCreateGoogleUserCreatesAccountWithoutPassword(t *testing.T) {
	svc, sessions, pool, _ := newService(t)
	ctx := context.Background()

	user, err := svc.FindOrCreateGoogleUser(ctx, auth.GoogleProfile{
		Subject: "google-sub-1",
		Email:   "oauth@north.test",
		Name:    "OAuth User",
	})
	if err != nil {
		t.Fatalf("FindOrCreateGoogleUser: %v", err)
	}
	if user.Email != "oauth@north.test" {
		t.Fatalf("email = %q", user.Email)
	}

	var hash *string
	if scanErr := pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", user.ID).Scan(&hash); scanErr != nil {
		t.Fatalf("scan password_hash: %v", scanErr)
	}
	if hash != nil {
		t.Fatalf("oauth-only account must have null password_hash, got %q", *hash)
	}

	var provider, subject string
	if scanErr := pool.QueryRow(ctx,
		`SELECT provider, provider_subject FROM auth_identities WHERE user_id = $1`, user.ID,
	).Scan(&provider, &subject); scanErr != nil {
		t.Fatalf("identity: %v", scanErr)
	}
	if provider != "google" || subject != "google-sub-1" {
		t.Fatalf("identity = %s/%s", provider, subject)
	}

	// Password login must fail for this account (same as wrong password).
	_, _, err = svc.Login(ctx, auth.LoginInput{
		Email: "oauth@north.test", Password: "correct horse battery staple",
	}, auth.Metadata{})
	if !apperr.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("password login on oauth account: %v", err)
	}

	// Second Google sign-in reuses the same user.
	again, err := svc.FindOrCreateGoogleUser(ctx, auth.GoogleProfile{
		Subject: "google-sub-1",
		Email:   "oauth@north.test",
		Name:    "OAuth User",
	})
	if err != nil {
		t.Fatalf("second FindOrCreate: %v", err)
	}
	if again.ID != user.ID {
		t.Fatal("expected same user for same google subject")
	}

	// Session can still be issued.
	token, _, err := sessions.Create(ctx, user.ID, auth.Metadata{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := sessions.Resolve(ctx, token); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

func TestFindOrCreateGoogleUserLinksExistingEmailAccount(t *testing.T) {
	svc, _, pool, _ := newService(t)
	ctx := context.Background()

	created, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	// Same email, new Google subject → link identity, do not create a second user.
	linked, err := svc.FindOrCreateGoogleUser(ctx, auth.GoogleProfile{
		Subject: "google-sub-linked",
		Email:   created.Email,
		Name:    "Different Name From Google",
	})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if linked.ID != created.ID {
		t.Fatal("should link to the existing password account")
	}

	var count int
	if scanErr := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count); scanErr != nil {
		t.Fatalf("count users: %v", scanErr)
	}
	if count != 1 {
		t.Fatalf("expected one user, got %d", count)
	}

	// Original password still works after linking.
	if _, _, loginErr := svc.Login(ctx, auth.LoginInput{
		Email: created.Email, Password: goodPassword,
	}, auth.Metadata{}); loginErr != nil {
		t.Fatalf("password login after link: %v", loginErr)
	}

	// Google subject now resolves to the same user.
	again, err := svc.FindOrCreateGoogleUser(ctx, auth.GoogleProfile{
		Subject: "google-sub-linked",
		Email:   created.Email,
		Name:    "OAuth",
	})
	if err != nil {
		t.Fatalf("resolve by subject: %v", err)
	}
	if again.ID != created.ID {
		t.Fatal("google subject should map to linked user")
	}
}

func TestFindOrCreateGoogleUserRejectsMissingSubject(t *testing.T) {
	svc, _, _, _ := newService(t)
	_, err := svc.FindOrCreateGoogleUser(context.Background(), auth.GoogleProfile{
		Email: "x@north.test",
		Name:  "X",
	})
	if err == nil {
		t.Fatal("expected error for missing subject")
	}
}
