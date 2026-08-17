package integrations

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	integrationsdb "github.com/NorthAIProject/north-client/internal/integrations/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/secret"
)

type Repository struct {
	q      *integrationsdb.Queries
	sealer *secret.Sealer
}

func NewRepository(pool *pgxpool.Pool, sealer *secret.Sealer) *Repository {
	return &Repository{q: integrationsdb.New(pool), sealer: sealer}
}

// Upsert stores or replaces a connection, sealing the token.
//
// Unlike strava_connections there is no plaintext column to fall back to: this
// table is new, so refusing to write without a key is free, and it means a
// deployment that forgot ENCRYPTION_KEY fails loudly at connect time rather
// than quietly storing credentials in the clear.
func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, provider, endpoint, token string) (Connection, error) {
	sealed, err := r.seal(userID, token)
	if err != nil {
		return Connection{}, err
	}

	row, err := r.q.UpsertIntegrationConnection(ctx, integrationsdb.UpsertIntegrationConnectionParams{
		UserID:      userID,
		Provider:    provider,
		Endpoint:    endpoint,
		TokenSealed: sealed,
	})
	if err != nil {
		return Connection{}, apperr.Wrap(err, "upsert integration connection")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, userID uuid.UUID, provider string) (Connection, error) {
	row, err := r.q.GetIntegrationConnection(ctx, integrationsdb.GetIntegrationConnectionParams{
		UserID:   userID,
		Provider: provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, apperr.ErrNotFound
		}
		return Connection{}, apperr.Wrap(err, "get integration connection")
	}
	return fromDB(row), nil
}

// Token opens the stored credential.
//
// Separate from Get so that every caller which only needs to know whether a
// connection exists — the settings page, the status card — cannot accidentally
// hold the secret.
func (r *Repository) Token(ctx context.Context, userID uuid.UUID, provider string) (string, error) {
	row, err := r.q.GetIntegrationConnection(ctx, integrationsdb.GetIntegrationConnectionParams{
		UserID:   userID,
		Provider: provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apperr.ErrNotFound
		}
		return "", apperr.Wrap(err, "get integration connection")
	}
	return r.open(userID, row.TokenSealed)
}

func (r *Repository) Delete(ctx context.Context, userID uuid.UUID, provider string) error {
	return apperr.Wrap(r.q.DeleteIntegrationConnection(ctx, integrationsdb.DeleteIntegrationConnectionParams{
		UserID:   userID,
		Provider: provider,
	}), "delete integration connection")
}

// MarkChecked records the outcome of the last attempt to reach the server.
func (r *Repository) MarkChecked(ctx context.Context, userID uuid.UUID, provider, status, reason string) error {
	return apperr.Wrap(r.q.MarkIntegrationChecked(ctx, integrationsdb.MarkIntegrationCheckedParams{
		UserID:    userID,
		Provider:  provider,
		Status:    status,
		LastError: reason,
	}), "mark integration checked")
}

// seal encrypts the token with the user id as additional data, so a row copied
// to another account fails to open rather than handing over their calendar.
func (r *Repository) seal(userID uuid.UUID, token string) ([]byte, error) {
	if token == "" {
		return nil, nil
	}
	if r.sealer == nil {
		return nil, apperr.Wrap(apperr.ErrValidation,
			"this process has no encryption key, so it cannot store an integration token")
	}
	sealed, err := r.sealer.Seal(userID[:], []byte(token))
	if err != nil {
		// Never wrapped with the token.
		return nil, errors.New("seal integration token")
	}
	return sealed, nil
}

func (r *Repository) open(userID uuid.UUID, sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil // a server that needs no auth
	}
	if r.sealer == nil {
		return "", errors.New("integration token is encrypted but this process has no encryption key")
	}
	token, err := r.sealer.Open(userID[:], sealed)
	if err != nil {
		return "", apperr.Wrap(err, "open integration token")
	}
	return string(token), nil
}

func fromDB(row integrationsdb.IntegrationConnection) Connection {
	return Connection{
		UserID:        row.UserID,
		Provider:      row.Provider,
		Endpoint:      row.Endpoint,
		Status:        row.Status,
		LastError:     row.LastError,
		LastCheckedAt: row.LastCheckedAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}
