package users

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Service holds account rules. It is the only place that decides what a valid
// profile looks like, so the web form and a future MCP tool cannot drift apart.
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ByID(ctx context.Context, id uuid.UUID) (User, error) {
	return s.repo.ByID(ctx, id)
}

func (s *Service) ByEmail(ctx context.Context, email string) (User, error) {
	return s.repo.ByEmail(ctx, email)
}

func (s *Service) CredentialsByEmail(ctx context.Context, email string) (User, string, error) {
	return s.repo.CredentialsByEmail(ctx, email)
}

// Registration is a validated signup request. PasswordHash is supplied by
// internal/auth; this package never handles plaintext. Empty PasswordHash is
// allowed for OAuth- and passkey-only accounts.
type Registration struct {
	// ID is optional; see NewRecord.ID.
	ID           uuid.UUID
	Email        string
	PasswordHash string
	DisplayName  string
	Timezone     string
}

// ValidateRegistration checks the account fields without touching the database.
//
// It exists so a caller can collect account problems alongside problems it
// found itself (a rejected password, say) and report them in one pass, without
// that collection having the side effect of creating an account.
func (s *Service) ValidateRegistration(reg Registration) (Registration, error) {
	var errs apperr.FieldErrors

	email := strings.TrimSpace(reg.Email)
	if email == "" {
		errs = errs.Add("email", "Email is required.")
	} else if !validEmail(email) {
		errs = errs.Add("email", "That does not look like a valid email address.")
	}

	name := strings.TrimSpace(reg.DisplayName)
	switch {
	case name == "":
		errs = errs.Add("display_name", "Name is required.")
	case len(name) > 100:
		errs = errs.Add("display_name", "Name must be 100 characters or fewer.")
	}

	tz := strings.TrimSpace(reg.Timezone)
	if tz == "" {
		tz = "UTC"
	} else if _, err := time.LoadLocation(tz); err != nil {
		errs = errs.Add("timezone", "Unknown timezone.")
	}

	if err := errs.OrNil(); err != nil {
		return Registration{}, err
	}

	// Returned normalised so the caller persists exactly what was validated.
	return Registration{
		ID:           reg.ID,
		Email:        email,
		PasswordHash: reg.PasswordHash,
		DisplayName:  name,
		Timezone:     tz,
	}, nil
}

func (s *Service) Register(ctx context.Context, reg Registration) (User, error) {
	clean, err := s.ValidateRegistration(reg)
	if err != nil {
		return User{}, err
	}

	return s.repo.Create(ctx, NewRecord(clean))
}

// UpdatePasswordHash replaces the stored hash. The caller (internal/auth) is
// responsible for hashing and for revoking sessions; this package only
// persists the opaque string.
func (s *Service) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	if hash == "" {
		return apperr.New("password hash is required")
	}
	return s.repo.UpdatePasswordHash(ctx, id, hash)
}

func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, p Profile) (User, error) {
	var errs apperr.FieldErrors

	name := strings.TrimSpace(p.DisplayName)
	switch {
	case name == "":
		errs = errs.Add("display_name", "Name is required.")
	case len(name) > 100:
		errs = errs.Add("display_name", "Name must be 100 characters or fewer.")
	}

	tz := strings.TrimSpace(p.Timezone)
	if tz == "" {
		tz = "UTC"
	} else if _, err := time.LoadLocation(tz); err != nil {
		errs = errs.Add("timezone", "Unknown timezone.")
	}

	// Long enough to describe a coaching preference, short enough that it
	// cannot crowd out real context in the prompt budget.
	if len(p.CoachingStyle) > 1000 {
		errs = errs.Add("coaching_style", "Keep this under 1000 characters.")
	}

	if err := errs.OrNil(); err != nil {
		return User{}, err
	}

	return s.repo.UpdateProfile(ctx, id, Profile{
		DisplayName:   name,
		Timezone:      tz,
		CoachingStyle: strings.TrimSpace(p.CoachingStyle),
	})
}

func validEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	// ParseAddress accepts `Name <a@b.c>`; a signup form should not.
	return err == nil && addr.Address == s
}
