package auth

import (
	"context"
	"strings"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Service coordinates credentials, accounts, and sessions. It is the only place
// that knows a signup produces both a user and a logged-in session.
type Service struct {
	users    *users.Service
	sessions *SessionStore
}

func NewService(userSvc *users.Service, sessions *SessionStore) *Service {
	return &Service{users: userSvc, sessions: sessions}
}

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

// Sessions exposes the store for middleware and background cleanup.
func (s *Service) Sessions() *SessionStore { return s.sessions }
