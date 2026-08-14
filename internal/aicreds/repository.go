package aicreds

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	aicredsdb "github.com/NorthAIProject/north-client/internal/aicreds/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Sealed is an encrypted key on its way to or from storage.
//
// A named type rather than a bare []byte so that a plaintext string cannot
// reach Upsert by mistake — the compiler enforces what a comment otherwise
// only asks for.
type Sealed []byte

type Repository struct {
	q *aicredsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: aicredsdb.New(pool)}
}

// Get returns the credential without its key. Use Key for the sealed bytes;
// the split means a caller that only renders the settings page never holds
// the ciphertext at all.
func (r *Repository) Get(ctx context.Context, userID uuid.UUID) (Credential, error) {
	row, err := r.q.GetUserAICredential(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Credential{}, apperr.ErrNotFound
		}
		return Credential{}, apperr.Wrap(err, "get user ai credential")
	}
	return fromDB(row), nil
}

// Key returns the sealed key alongside the credential.
func (r *Repository) Key(ctx context.Context, userID uuid.UUID) (Credential, Sealed, error) {
	row, err := r.q.GetUserAICredential(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Credential{}, nil, apperr.ErrNotFound
		}
		return Credential{}, nil, apperr.Wrap(err, "get user ai credential")
	}
	return fromDB(row), Sealed(row.ApiKey), nil
}

// Upsert writes the credential.
//
// The error is not wrapped with the parameters, following
// strava/repository.go: one of them is a credential, and an error string is
// where a credential-shaped value most easily ends up in a log.
func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, provider string, key Sealed, hint, model string) (Credential, error) {
	row, err := r.q.UpsertUserAICredential(ctx, aicredsdb.UpsertUserAICredentialParams{
		UserID: userID, Provider: provider, ApiKey: key, KeyHint: hint, Model: model,
	})
	if err != nil {
		return Credential{}, errors.New("upsert user ai credential: query failed")
	}
	return fromDB(row), nil
}

// UpdateModel changes the model without touching the stored key.
func (r *Repository) UpdateModel(ctx context.Context, userID uuid.UUID, model string) (Credential, error) {
	row, err := r.q.UpdateUserAICredentialModel(ctx, aicredsdb.UpdateUserAICredentialModelParams{
		UserID: userID, Model: model,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Credential{}, apperr.ErrNotFound
		}
		return Credential{}, apperr.Wrap(err, "update user ai credential model")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, userID uuid.UUID) error {
	rows, err := r.q.DeleteUserAICredential(ctx, userID)
	if err != nil {
		return apperr.Wrap(err, "delete user ai credential")
	}
	if rows == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// RecordError stores why the last attempt failed. Best-effort by design: the
// caller is already handling a failure and must not acquire a second one.
func (r *Repository) RecordError(ctx context.Context, userID uuid.UUID, reason string) error {
	err := r.q.RecordUserAICredentialError(ctx, aicredsdb.RecordUserAICredentialErrorParams{
		UserID: userID, LastError: reason,
	})
	return apperr.Wrap(err, "record user ai credential error")
}

func fromDB(row aicredsdb.UserAiCredential) Credential {
	return Credential{
		UserID:      row.UserID,
		Provider:    row.Provider,
		KeyHint:     row.KeyHint,
		Model:       row.Model,
		LastError:   row.LastError,
		LastErrorAt: row.LastErrorAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
