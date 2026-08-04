package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

const goodPassword = "correct horse battery staple"

func newService(t *testing.T) (*auth.Service, *auth.SessionStore, *pgxpool.Pool, *auth.CaptureMailer) {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))
	sessions := auth.NewSessionStore(pool, time.Hour)
	mailer := &auth.CaptureMailer{}

	svc := auth.NewService(userSvc, sessions, auth.ServiceOptions{
		Mailer:  mailer,
		BaseURL: "http://north.test",
	})
	return svc, sessions, pool, mailer
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
	svc, sessions, _, _ := newService(t)
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
	svc, _, _, _ := newService(t)
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
	svc, _, _, _ := newService(t)

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
	svc, _, _, _ := newService(t)

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

// Regression: an earlier version collected account-field errors by calling
// Register with a placeholder password hash. That created a real account with
// an unusable hash, so a user who mistyped their password on the first attempt
// could never sign up or log in with that address again.
func TestRejectedSignupCreatesNoAccount(t *testing.T) {
	svc, _, pool, _ := newService(t)
	ctx := context.Background()

	in := validSignup()
	in.Password = "short"
	in.PasswordConfirmation = "short"

	if _, _, err := svc.Signup(ctx, in, auth.Metadata{}); err == nil {
		t.Fatal("a short password must be rejected")
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("a rejected signup must write nothing; found %d user row(s)", count)
	}

	// The address must still be usable once the password is fixed.
	if _, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{}); err != nil {
		t.Fatalf("retrying with a valid password should succeed: %v", err)
	}
}

func TestLoginSucceedsWithCorrectPassword(t *testing.T) {
	svc, sessions, _, _ := newService(t)
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
	svc, _, _, _ := newService(t)
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
	svc, sessions, _, _ := newService(t)
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
	svc := auth.NewService(userSvc, expired, auth.ServiceOptions{BaseURL: "http://north.test"})

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
	svc, sessions, _, _ := newService(t)
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

	user, liveToken, err := auth.NewService(userSvc, live, auth.ServiceOptions{BaseURL: "http://north.test"}).Signup(ctx, validSignup(), auth.Metadata{})
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
	svc, sessions, pool, _ := newService(t)
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

func TestRequestPasswordResetSendsLinkForKnownEmail(t *testing.T) {
	svc, _, _, mailer := newService(t)
	ctx := context.Background()

	if _, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{}); err != nil {
		t.Fatalf("signup: %v", err)
	}

	if err := svc.RequestPasswordReset(ctx, "fernando@north.test"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	if len(mailer.Messages) != 1 {
		t.Fatalf("expected one email, got %d", len(mailer.Messages))
	}
	msg := mailer.Messages[0]
	if msg.To != "fernando@north.test" {
		t.Fatalf("to = %q", msg.To)
	}
	if !strings.Contains(msg.Body, "http://north.test/reset-password?token=") {
		t.Fatalf("email body missing reset URL: %q", msg.Body)
	}
}

func TestRequestPasswordResetIsSilentForUnknownEmail(t *testing.T) {
	svc, _, _, mailer := newService(t)

	if err := svc.RequestPasswordReset(context.Background(), "nobody@north.test"); err != nil {
		t.Fatalf("unknown email must still succeed: %v", err)
	}
	if len(mailer.Messages) != 0 {
		t.Fatalf("must not send mail for unknown addresses, sent %d", len(mailer.Messages))
	}
}

func TestRequestPasswordResetRejectsBadEmail(t *testing.T) {
	svc, _, _, _ := newService(t)

	err := svc.RequestPasswordReset(context.Background(), "not-an-email")
	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field errors, got %v", err)
	}
	if _, ok := fieldErrs.Messages()["email"]; !ok {
		t.Fatalf("expected email field error, got %v", fieldErrs.Messages())
	}
}

func TestResetPasswordChangesPasswordRevokesSessionsAndSignsIn(t *testing.T) {
	svc, sessions, _, mailer := newService(t)
	ctx := context.Background()

	user, oldToken, err := svc.Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	if err := svc.RequestPasswordReset(ctx, user.Email); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	raw := extractResetToken(t, mailer.Messages[0].Body)

	const newPassword = "a brand new horse battery"
	resetUser, newToken, err := svc.ResetPassword(ctx, auth.ResetPasswordInput{
		Token:                raw,
		Password:             newPassword,
		PasswordConfirmation: newPassword,
	}, auth.Metadata{})
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if resetUser.ID != user.ID {
		t.Fatal("reset signed in the wrong user")
	}

	// Old sessions must die so a stolen cookie cannot outlive a password change.
	if _, err := sessions.Resolve(ctx, oldToken); !apperr.Is(err, apperr.ErrUnauthenticated) {
		t.Fatalf("pre-reset session must be revoked, got %v", err)
	}
	if _, err := sessions.Resolve(ctx, newToken); err != nil {
		t.Fatalf("reset should sign the user in: %v", err)
	}

	// Old password fails; new password works.
	if _, _, err := svc.Login(ctx, auth.LoginInput{Email: user.Email, Password: goodPassword}, auth.Metadata{}); !apperr.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old password must fail, got %v", err)
	}
	if _, _, err := svc.Login(ctx, auth.LoginInput{Email: user.Email, Password: newPassword}, auth.Metadata{}); err != nil {
		t.Fatalf("new password must work: %v", err)
	}

	// Token is single-use.
	if _, _, err := svc.ResetPassword(ctx, auth.ResetPasswordInput{
		Token:                raw,
		Password:             newPassword + " again",
		PasswordConfirmation: newPassword + " again",
	}, auth.Metadata{}); !apperr.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("reusing a token must fail, got %v", err)
	}
}

func TestResetPasswordRejectsInvalidToken(t *testing.T) {
	svc, _, _, _ := newService(t)

	_, _, err := svc.ResetPassword(context.Background(), auth.ResetPasswordInput{
		Token:                "not-a-real-token",
		Password:             goodPassword,
		PasswordConfirmation: goodPassword,
	}, auth.Metadata{})
	if !apperr.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken, got %v", err)
	}
}

func extractResetToken(t *testing.T, body string) string {
	t.Helper()
	const marker = "http://north.test/reset-password?token="
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("reset URL not found in body: %q", body)
	}
	rest := body[idx+len(marker):]
	// Token is the first non-whitespace run.
	end := strings.IndexAny(rest, " \n\r\t")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
