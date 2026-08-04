package users

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	usersdb "github.com/NorthAIProject/north-client/internal/users/db"
)

// uniqueViolation is Postgres' SQLSTATE for a unique constraint breach.
const uniqueViolation = "23505"

// Repository owns persistence for accounts. It translates database errors into
// the shared sentinels and generated rows into domain types, so that no pgx or
// sqlc type ever reaches a service.
type Repository struct {
	q *usersdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: usersdb.New(pool)}
}

// NewRecord is everything needed to create an account. PasswordHash is already
// hashed; this package never sees a plaintext password. An empty PasswordHash
// stores NULL, which is correct for OAuth- or passkey-only accounts.
type NewRecord struct {
	// ID is optional. When set, the row is inserted with that primary key so a
	// WebAuthn registration can use the same handle that the authenticator stored.
	ID           uuid.UUID
	Email        string
	PasswordHash string
	DisplayName  string
	Timezone     string
}

func (r *Repository) Create(ctx context.Context, rec NewRecord) (User, error) {
	email := normalizeEmail(rec.Email)
	name := strings.TrimSpace(rec.DisplayName)
	passwordHash := optionalHash(rec.PasswordHash)

	var (
		row usersdb.User
		err error
	)
	if rec.ID != uuid.Nil {
		row, err = r.q.CreateUserWithID(ctx, usersdb.CreateUserWithIDParams{
			ID:           rec.ID,
			Email:        email,
			PasswordHash: passwordHash,
			DisplayName:  name,
			Timezone:     rec.Timezone,
		})
	} else {
		row, err = r.q.CreateUser(ctx, usersdb.CreateUserParams{
			Email:        email,
			PasswordHash: passwordHash,
			DisplayName:  name,
			Timezone:     rec.Timezone,
		})
	}
	if err != nil {
		// Relying on the unique index rather than a prior existence check keeps
		// signup correct under concurrency: two simultaneous requests for the
		// same address cannot both succeed.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return User{}, apperr.Wrap(apperr.ErrConflict, "email already registered")
		}
		return User{}, apperr.Wrap(err, "create user")
	}
	return fromDB(row), nil
}

func (r *Repository) ByID(ctx context.Context, id uuid.UUID) (User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperr.ErrNotFound
		}
		return User{}, apperr.Wrap(err, "get user by id")
	}
	return fromDB(row), nil
}

func (r *Repository) ByEmail(ctx context.Context, email string) (User, error) {
	row, err := r.q.GetUserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperr.ErrNotFound
		}
		return User{}, apperr.Wrap(err, "get user by email")
	}
	return fromDB(row), nil
}

// CredentialsByEmail returns the user together with the stored password hash.
// It is separate from ByEmail so the hash is only loaded on the one code path
// that needs it: verifying a login. A null column becomes "" so password login
// for OAuth/passkey-only accounts fails the same way as a wrong password.
func (r *Repository) CredentialsByEmail(ctx context.Context, email string) (User, string, error) {
	row, err := r.q.GetUserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, "", apperr.ErrNotFound
		}
		return User{}, "", apperr.Wrap(err, "get credentials by email")
	}
	return fromDB(row), derefString(row.PasswordHash), nil
}

// Profile carries the user-editable fields of an account.
type Profile struct {
	DisplayName   string
	Timezone      string
	CoachingStyle string
}

func (r *Repository) UpdateProfile(ctx context.Context, id uuid.UUID, p Profile) (User, error) {
	var style *string
	if s := strings.TrimSpace(p.CoachingStyle); s != "" {
		style = &s
	}

	row, err := r.q.UpdateUserProfile(ctx, usersdb.UpdateUserProfileParams{
		ID:            id,
		DisplayName:   strings.TrimSpace(p.DisplayName),
		Timezone:      p.Timezone,
		CoachingStyle: style,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperr.ErrNotFound
		}
		return User{}, apperr.Wrap(err, "update profile")
	}
	return fromDB(row), nil
}

func (r *Repository) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	err := r.q.UpdateUserPassword(ctx, usersdb.UpdateUserPasswordParams{
		ID:           id,
		PasswordHash: optionalHash(hash),
	})
	return apperr.Wrap(err, "update password")
}

// normalizeEmail trims and lowercases. The column is citext so comparison is
// already case-insensitive; normalising on write keeps what is displayed back
// to the user consistent with what they typed at signup.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func optionalHash(hash string) *string {
	if hash == "" {
		return nil
	}
	return &hash
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
