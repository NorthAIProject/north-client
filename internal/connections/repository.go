package connections

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	connectionsdb "github.com/NorthAIProject/north-client/internal/connections/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *connectionsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: connectionsdb.New(pool)}
}

// Insert stores a connection. The hash is the only form of the token that
// reaches here; the caller keeps the plaintext just long enough to show it.
func (r *Repository) Insert(ctx context.Context, userID uuid.UUID, name string, kind ClientKind, tokenHash []byte, prefix string) (Connection, error) {
	row, err := r.q.InsertAgentConnection(ctx, connectionsdb.InsertAgentConnectionParams{
		UserID: userID, Name: name, ClientKind: string(kind), TokenHash: tokenHash, TokenPrefix: prefix,
	})
	if err != nil {
		return Connection{}, apperr.Wrap(err, "insert agent connection")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID) ([]Connection, error) {
	rows, err := r.q.ListAgentConnections(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list agent connections")
	}
	out := make([]Connection, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

// ByTokenHash finds the live connection a presented token belongs to.
//
// The error is deliberately not wrapped with the hash: an error string is the
// one place a credential-shaped value tends to end up in a log.
func (r *Repository) ByTokenHash(ctx context.Context, tokenHash []byte) (Connection, error) {
	row, err := r.q.GetAgentConnectionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, apperr.ErrNotFound
		}
		return Connection{}, errors.New("get agent connection by token hash: query failed")
	}
	return fromDB(row), nil
}

// Touch records that a token was used. The query is a no-op unless the stored
// value has gone stale, so a polling agent does not write on every request.
func (r *Repository) Touch(ctx context.Context, id uuid.UUID) error {
	if err := r.q.TouchAgentConnection(ctx, id); err != nil {
		return apperr.Wrap(err, "touch agent connection")
	}
	return nil
}

// Revoke turns a connection off. Scoped by user as well as id, so an id
// guessed from someone else's page revokes nothing.
func (r *Repository) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	rows, err := r.q.RevokeAgentConnection(ctx, connectionsdb.RevokeAgentConnectionParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "revoke agent connection")
	}
	if rows == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func fromDB(row connectionsdb.AgentConnection) Connection {
	return Connection{
		ID:          row.ID,
		UserID:      row.UserID,
		Name:        row.Name,
		Kind:        ClientKind(row.ClientKind),
		TokenPrefix: row.TokenPrefix,
		CreatedAt:   row.CreatedAt,
		LastUsedAt:  row.LastUsedAt,
	}
}
