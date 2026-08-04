package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	authdb "github.com/NorthAIProject/north-client/internal/auth/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// passwordResetTTL is how long a reset link stays valid. Short enough that a
// leaked inbox is a brief window; long enough to cover slow email delivery.
const passwordResetTTL = time.Hour

// passwordResetTokenBytes matches session token entropy.
const passwordResetTokenBytes = 32

// Service coordinates credentials, accounts, and sessions. It is the only place
// that knows a signup produces both a user and a logged-in session.
type Service struct {
	users    *users.Service
	sessions *SessionStore
	mailer   Mailer
	baseURL  string
	log      *slog.Logger

	google   *googleOAuth
	webauthn *passkeyAuth
}

// ServiceOptions wires optional infrastructure for auth journeys that need it
// (password reset email, absolute reset links, Google OAuth, WebAuthn).
type ServiceOptions struct {
	Mailer  Mailer
	BaseURL string
	Log     *slog.Logger

	// Google OAuth. When ClientID or ClientSecret is empty, Google sign-in is
	// disabled and its routes return 404.
	GoogleClientID     string
	GoogleClientSecret string

	// WebAuthn relying party. RP ID defaults to the host of BaseURL; Origins
	// defaults to BaseURL. Display name defaults to "North".
	WebAuthnRPID        string
	WebAuthnRPOrigins   []string
	WebAuthnDisplayName string
}

// NewService builds the auth service. opts may be zero; a LogMailer is used
// when no Mailer is provided so local development still prints reset links.
func NewService(userSvc *users.Service, sessions *SessionStore, opts ServiceOptions) *Service {
	mailer := opts.Mailer
	if mailer == nil {
		mailer = LogMailer{Log: opts.Log}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8090"
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	s := &Service{
		users:    userSvc,
		sessions: sessions,
		mailer:   mailer,
		baseURL:  baseURL,
		log:      log,
	}
	s.google = newGoogleOAuth(opts.GoogleClientID, opts.GoogleClientSecret, baseURL)
	s.webauthn = newPasskeyAuth(sessions, opts, baseURL, log)
	return s
}

// GoogleEnabled reports whether Google OAuth credentials are configured.
func (s *Service) GoogleEnabled() bool { return s.google != nil && s.google.enabled() }

// PasskeyEnabled reports whether WebAuthn is configured (always true when the
// service starts with a valid BASE_URL).
func (s *Service) PasskeyEnabled() bool { return s.webauthn != nil && s.webauthn.enabled() }

// SignupInput is a raw signup form.
type SignupInput struct {
	Email                string
	DisplayName          string
	Password             string
	PasswordConfirmation string
	Timezone             string
}

// Signup creates an account and immediately issues a session, so a new user
// lands in the application rather than on a login form.
func (s *Service) Signup(ctx context.Context, in SignupInput, meta Metadata) (users.User, string, error) {
	var errs apperr.FieldErrors

	// Both halves are validated before anything is written, so every problem is
	// reported in one pass and a rejected signup leaves no trace. Validation
	// must never have side effects: an earlier version collected account errors
	// by calling Register with a placeholder hash, which created a real account
	// with an unusable password and locked that address out permanently.
	registration := users.Registration{
		Email:       in.Email,
		DisplayName: in.DisplayName,
		Timezone:    in.Timezone,
	}
	if _, err := s.users.ValidateRegistration(registration); err != nil {
		var fieldErrs apperr.FieldErrors
		if !apperr.As(err, &fieldErrs) {
			return users.User{}, "", err
		}
		errs = append(errs, fieldErrs...)
	}

	if err := ValidatePassword(in.Password); err != nil {
		var fieldErrs apperr.FieldErrors
		if !apperr.As(err, &fieldErrs) {
			return users.User{}, "", err
		}
		errs = append(errs, fieldErrs...)
	} else if !ConfirmationMatches(in.Password, in.PasswordConfirmation) {
		errs = errs.Add("password_confirmation", "Passwords do not match.")
	}

	if err := errs.OrNil(); err != nil {
		return users.User{}, "", err
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return users.User{}, "", err
	}

	registration.PasswordHash = hash

	user, err := s.users.Register(ctx, registration)
	if err != nil {
		if apperr.Is(err, apperr.ErrConflict) {
			// Reported against the field so the form can highlight it. This does
			// disclose that an address is registered, which is unavoidable for a
			// usable signup form: a silent success would strand the real owner.
			return users.User{}, "", apperr.FieldErrors{{
				Field:   "email",
				Message: "An account with that email already exists.",
			}}
		}
		return users.User{}, "", err
	}

	token, _, err := s.sessions.Create(ctx, user.ID, meta)
	if err != nil {
		return users.User{}, "", err
	}

	return user, token, nil
}

// LoginInput is a raw login form.
type LoginInput struct {
	Email    string
	Password string
}

// ErrInvalidCredentials is returned for any failed login.
//
// It is deliberately one error for both "no such account" and "wrong password":
// distinguishing them turns the login form into an account-enumeration oracle.
var ErrInvalidCredentials = apperr.New("email or password is incorrect")

// ErrInvalidResetToken is returned when a reset link is missing, expired, or
// already used. The three are not distinguished to avoid probing.
var ErrInvalidResetToken = apperr.New("this reset link is invalid or has expired")

func (s *Service) Login(ctx context.Context, in LoginInput, meta Metadata) (users.User, string, error) {
	email := strings.TrimSpace(in.Email)
	if email == "" || in.Password == "" {
		return users.User{}, "", ErrInvalidCredentials
	}

	user, hash, err := s.users.CredentialsByEmail(ctx, email)
	if err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		return users.User{}, "", err
	}

	// Runs even when the account does not exist. VerifyPassword hashes anyway
	// on an empty stored hash, so both paths cost the same and the response
	// time does not reveal which addresses are registered.
	if !VerifyPassword(hash, in.Password) {
		return users.User{}, "", ErrInvalidCredentials
	}

	token, _, err := s.sessions.Create(ctx, user.ID, meta)
	if err != nil {
		return users.User{}, "", err
	}

	return user, token, nil
}

// Logout ends the current session.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.sessions.Revoke(ctx, token)
}

// RequestPasswordReset starts the forget-password journey.
//
// It always returns nil for a well-formed email, whether or not an account
// exists, so the form cannot be used to probe registered addresses. Real send
// failures for known accounts are logged, not surfaced to the client.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return apperr.FieldErrors{{Field: "email", Message: "Email is required."}}
	}
	if !looksLikeEmail(email) {
		return apperr.FieldErrors{{Field: "email", Message: "That does not look like a valid email address."}}
	}

	user, err := s.users.ByEmail(ctx, email)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return nil
		}
		return err
	}

	raw, err := newOpaqueToken(passwordResetTokenBytes)
	if err != nil {
		return err
	}

	// Only the latest link for this account should work.
	if err := s.sessions.q.DeletePasswordResetTokensForUser(ctx, user.ID); err != nil {
		return apperr.Wrap(err, "clear prior reset tokens")
	}

	expiresAt := time.Now().Add(passwordResetTTL)
	if _, err := s.sessions.q.CreatePasswordResetToken(ctx, authdb.CreatePasswordResetTokenParams{
		TokenHash: hashToken(raw),
		UserID:    user.ID,
		ExpiresAt: expiresAt,
	}); err != nil {
		return apperr.Wrap(err, "create password reset token")
	}

	resetURL := s.baseURL + "/reset-password?token=" + url.QueryEscape(raw)
	msg := passwordResetEmail(user.FirstName(), resetURL)
	msg.To = user.Email

	if err := s.mailer.Send(ctx, msg); err != nil {
		// Do not fail the request: the client already got the generic success
		// path contract. Ops can retry from logs; the token is still valid.
		s.log.ErrorContext(ctx, "password reset email failed",
			slog.String("user_id", user.ID.String()),
			slog.Any("error", err),
		)
	}

	return nil
}

// ResetPasswordInput is the set-new-password form from a reset link.
type ResetPasswordInput struct {
	Token                string
	Password             string
	PasswordConfirmation string
}

// ResetPassword consumes a one-time token, sets a new password, revokes every
// existing session, and issues a fresh session so the user lands signed in.
func (s *Service) ResetPassword(ctx context.Context, in ResetPasswordInput, meta Metadata) (users.User, string, error) {
	token := strings.TrimSpace(in.Token)
	if token == "" {
		return users.User{}, "", ErrInvalidResetToken
	}

	var errs apperr.FieldErrors
	if err := ValidatePassword(in.Password); err != nil {
		var fieldErrs apperr.FieldErrors
		if !apperr.As(err, &fieldErrs) {
			return users.User{}, "", err
		}
		errs = append(errs, fieldErrs...)
	} else if !ConfirmationMatches(in.Password, in.PasswordConfirmation) {
		errs = errs.Add("password_confirmation", "Passwords do not match.")
	}
	if err := errs.OrNil(); err != nil {
		return users.User{}, "", err
	}

	row, err := s.sessions.q.GetPasswordResetToken(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return users.User{}, "", ErrInvalidResetToken
		}
		return users.User{}, "", apperr.Wrap(err, "lookup password reset token")
	}

	// Consume first so concurrent POSTs with the same link cannot both change
	// the password. A 0-row update means another request already used it.
	n, err := s.sessions.q.MarkPasswordResetTokenUsed(ctx, hashToken(token))
	if err != nil {
		return users.User{}, "", apperr.Wrap(err, "mark reset token used")
	}
	if n == 0 {
		return users.User{}, "", ErrInvalidResetToken
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return users.User{}, "", err
	}

	if err = s.users.UpdatePasswordHash(ctx, row.UserID, hash); err != nil {
		return users.User{}, "", err
	}

	if err = s.sessions.q.DeletePasswordResetTokensForUser(ctx, row.UserID); err != nil {
		// Password already changed; do not leave the user mid-flow.
		s.log.ErrorContext(ctx, "clear reset tokens after password change",
			slog.String("user_id", row.UserID.String()),
			slog.Any("error", err),
		)
	}

	if err = s.sessions.RevokeAll(ctx, row.UserID); err != nil {
		return users.User{}, "", err
	}

	user, err := s.users.ByID(ctx, row.UserID)
	if err != nil {
		return users.User{}, "", err
	}

	sessionToken, _, err := s.sessions.Create(ctx, user.ID, meta)
	if err != nil {
		return users.User{}, "", err
	}

	return user, sessionToken, nil
}

// Sessions exposes the store for middleware and background cleanup.
func (s *Service) Sessions() *SessionStore { return s.sessions }

func newOpaqueToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", apperr.Wrap(err, "generate token")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func looksLikeEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}
