package vault

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	vaultdb "github.com/NorthAIProject/north-client/internal/vault/db"
)

// Connection is a user's linked vault folder.
type Connection struct {
	UserID     uuid.UUID
	RootPath   string
	LastSyncAt *time.Time
	LastError  string
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Repository struct {
	q *vaultdb.Queries
}

func NewRepository(q *vaultdb.Queries) *Repository {
	return &Repository{q: q}
}

func (r *Repository) Get(ctx context.Context, userID uuid.UUID) (Connection, error) {
	row, err := r.q.GetVaultConnection(ctx, userID)
	if err != nil {
		return Connection{}, mapNotFound(err)
	}
	return fromDB(row), nil
}

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, rootPath string) (Connection, error) {
	row, err := r.q.UpsertVaultConnection(ctx, vaultdb.UpsertVaultConnectionParams{
		UserID:   userID,
		RootPath: rootPath,
	})
	if err != nil {
		return Connection{}, apperr.Wrap(err, "save vault connection")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, userID uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteVaultConnection(ctx, userID), "disconnect vault")
}

func (r *Repository) MarkSynced(ctx context.Context, userID uuid.UUID) error {
	return apperr.Wrap(r.q.MarkVaultSynced(ctx, userID), "mark vault synced")
}

func (r *Repository) MarkFailed(ctx context.Context, userID uuid.UUID, reason string) error {
	return apperr.Wrap(r.q.MarkVaultSyncFailed(ctx, vaultdb.MarkVaultSyncFailedParams{
		UserID:    userID,
		LastError: reason,
	}), "mark vault sync failed")
}

func fromDB(row vaultdb.VaultConnection) Connection {
	return Connection{
		UserID:     row.UserID,
		RootPath:   row.RootPath,
		LastSyncAt: row.LastSyncAt,
		LastError:  row.LastError,
		Enabled:    row.Enabled,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.ErrNotFound
	}
	return apperr.Wrap(err, "vault connection")
}
