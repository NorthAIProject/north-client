package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

const goodPassword = "correct horse battery staple"

func newService(t *testing.T) (*auth.Service, *auth.SessionStore, *pgxpool.Pool) {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))
	sessions := auth.NewSessionStore(pool, time.Hour)

	return auth.NewService(userSvc, sessions), sessions, pool
}

func validSignup() auth.SignupInput {
	return auth.SignupInput{
		Email:                "fernando@north.test",
		DisplayName:          "Fernando Correia",
		Password:             goodPassword,
		PasswordConfirmation: goodPassword,
		Timezone:             "Europe/Lisbon",
	}
}

func TestSignupCreatesAccountAndSession(t *testing.T) {
	svc, sessions, _ := newService(t)
	ctx := context.Background()

	user, token, err := svc.Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	if user.Email != "fernando@north.test" {
		t.Fatalf("email = %q", user.Email)
	}
	if user.Timezone != "Europe/Lisbon" {
		t.Fatalf("timezone = %q", user.Timezone)
	}

	// Signup should sign the user in, not hand them back to a login form.
	session, err := sessions.Resolve(ctx, token)
	if err != nil {
		t.Fatalf("the token returned by signup should resolve: %v", err)
	}
	if session.User.ID != user.ID {
		t.Fatal("the session resolved to a different user")
	}
}

func TestSignupRejectsDuplicateEmailCaseInsensitively(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	if _, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{}); err != nil {
		t.Fatalf("first signup: %v", err)
	}

	// citext plus normalisation on write: a different case is the same account.
	second := validSignup()
	second.Email = "Fernando@North.Test"

	_, _, err := svc.Signup(ctx, second, auth.Metadata{})
	if err == nil {
		t.Fatal("a duplicate email must be rejected")
	}

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field errors so the form can highlight the input, got %T: %v", err, err)
	}
	if _, ok := fieldErrs.Messages()["email"]; !ok {
		t.Fatalf("the failure should be attributed to the email field, got %v", fieldErrs.Messages())
	}
}

func TestSignupReportsEveryProblemAtOnce(t *testing.T) {
	svc, _, _ := newService(t)

	in := auth.SignupInput{
		Email:                "not-an-email",
		DisplayName:          "",
		Password:             "short",
		PasswordConfirmation: "short",
		Timezone:             "Mars/Olympus_Mons",
	}

	_, _, err := svc.Signup(context.Background(), in, auth.Metadata{})
	if err == nil {
		t.Fatal("expected rejection")
	}

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field errors, got %T", err)
	}

	msgs := fieldErrs.Messages()
	// The point of collecting failures is that the user fixes them in one pass
	// rather than discovering them one submit at a time.
	for _, field := range []string{"email", "display_name", "password", "timezone"} {
		if _, ok := msgs[field]; !ok {
			t.Errorf("expected a message for %q; got %v", field, msgs)
		}
	}
}

func TestSignupRejectsMismatchedConfirmation(t *testing.T) {
	svc, _, _ := newService(t)

	in := validSignup()
	in.PasswordConfirmation = goodPassword + " but different"

	_, _, err := svc.Signup(context.Background(), in, auth.Metadata{})
	if err == nil {
		t.Fatal("a mismatched confirmation must be rejected")
	}

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field errors, got %T", err)
	}
	if _, ok := fieldErrs.Messages()["password_confirmation"]; !ok {
		t.Fatalf("expected the confirmation field to be blamed, got %v", fieldErrs.Messages())
	}
}

func TestLoginSucceedsWithCorrectPassword(t *testing.T) {
	svc, sessions, _ := newService(t)
	ctx := context.Background()

	created, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	user, token, err := svc.Login(ctx, auth.LoginInput{
		Email:    "fernando@north.test",
		Password: goodPassword,
	}, auth.Metadata{UserAgent: "test-agent", IP: "203.0.113.7"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if user.ID != created.ID {
		t.Fatal("logged in as the wrong user")
	}
	if _, err := sessions.Resolve(ctx, token); err != nil {
		t.Fatalf("login token should resolve: %v", err)
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	if _, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{}); err != nil {
		t.Fatalf("signup: %v", err)
	}

	_, _, wrongPassword := svc.Login(ctx, auth.LoginInput{
		Email: "fernando@north.test", Password: "definitely not the password",
	}, auth.Metadata{})

	_, _, unknownAccount := svc.Login(ctx, auth.LoginInput{
		Email: "nobody@north.test", Password: "definitely not the password",
	}, auth.Metadata{})

	if wrongPassword == nil || unknownAccount == nil {
		t.Fatal("both attempts must fail")
	}

	// Distinguishing the two turns the login form into an oracle for which
	// email addresses are registered.
	if wrongPassword.Error() != unknownAccount.Error() {
		t.Fatalf("failures must be indistinguishable: %q vs %q", wrongPassword, unknownAccount)
	}
	if !apperr.Is(wrongPassword, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", wrongPassword)
	}
}

func TestLogoutRevokesOnlyThatSession(t *testing.T) {
	svc, sessions, _ := newService(t)
	ctx := context.Background()

	if _, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{}); err != nil {
		t.Fatalf("signup: %v", err)
	}

	_, laptop, err := svc.Login(ctx, auth.LoginInput{Email: "fernando@north.test", Password: goodPassword}, auth.Metadata{})
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	_, phone, err := svc.Login(ctx, auth.LoginInput{Email: "fernando@north.test", Password: goodPassword}, auth.Metadata{})
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	if err := svc.Logout(ctx, laptop); err != nil {
		t.Fatalf("logout: %v", err)
	}

	if _, err := sessions.Resolve(ctx, laptop); !apperr.Is(err, apperr.ErrUnauthenticated) {
		t.Fatalf("the signed-out session must be dead, got %v", err)
	}
	// Signing out of one device must not sign the user out everywhere.
	if _, err := sessions.Resolve(ctx, phone); err != nil {
		t.Fatalf("the other session should still work: %v", err)
	}
}

func TestResolveRejectsUnknownAndExpiredTokens(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))

	// A lifetime already in the past, so the row is created expired.
	expired := auth.NewSessionStore(pool, -time.Minute)
	svc := auth.NewService(userSvc, expired)

	_, token, err := svc.Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	if _, err := expired.Resolve(ctx, token); !apperr.Is(err, apperr.ErrUnauthenticated) {
		t.Fatalf("an expired session must not resolve, got %v", err)
	}

	for _, bad := range []string{"", "not-a-real-token", token + "x"} {
		if _, err := expired.Resolve(ctx, bad); !apperr.Is(err, apperr.ErrUnauthenticated) {
			t.Fatalf("token %q should be rejected, got %v", bad, err)
		}
	}
}

func TestRevokeAllEndsEverySession(t *testing.T) {
	svc, sessions, _ := newService(t)
	ctx := context.Background()

	user, first, err := svc.Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	_, second, err := svc.Login(ctx, auth.LoginInput{Email: "fernando@north.test", Password: goodPassword}, auth.Metadata{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if err := sessions.RevokeAll(ctx, user.ID); err != nil {
		t.Fatalf("revoke all: %v", err)
	}

	for name, token := range map[string]string{"first": first, "second": second} {
		if _, err := sessions.Resolve(ctx, token); !apperr.Is(err, apperr.ErrUnauthenticated) {
			t.Fatalf("the %s session should be revoked, got %v", name, err)
		}
	}
}

func TestPurgeExpiredRemovesOnlyExpiredRows(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))

	live := auth.NewSessionStore(pool, time.Hour)
	dead := auth.NewSessionStore(pool, -time.Minute)

	user, liveToken, err := auth.NewService(userSvc, live).Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if _, _, err := dead.Create(ctx, user.ID, auth.Metadata{}); err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	removed, err := live.PurgeExpired(ctx)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected to purge exactly the expired row, purged %d", removed)
	}
	if _, err := live.Resolve(ctx, liveToken); err != nil {
		t.Fatalf("the live session must survive the purge: %v", err)
	}
}

func TestSessionsDieWithTheirUser(t *testing.T) {
	svc, sessions, pool := newService(t)
	ctx := context.Background()

	user, token, err := svc.Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	// ON DELETE CASCADE is what stops a deleted account leaving live sessions
	// behind. That guarantee lives in the schema, so it is worth asserting.
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	if _, err := sessions.Resolve(ctx, token); !apperr.Is(err, apperr.ErrUnauthenticated) {
		t.Fatalf("deleting the user must invalidate the session, got %v", err)
	}
}
