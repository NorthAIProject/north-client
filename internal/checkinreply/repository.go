package checkinreply

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	checkinreplydb "github.com/NorthAIProject/north-client/internal/checkinreply/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *checkinreplydb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: checkinreplydb.New(pool)}
}

// stored is a note with the content it was written for.
type stored struct {
	Reply
	ContentHash string
}

// Get is the note stored for a check-in. found is false when there is none.
func (r *Repository) Get(ctx context.Context, checkInID, userID uuid.UUID) (stored, bool, error) {
	row, err := r.q.GetReply(ctx, checkinreplydb.GetReplyParams{CheckInID: checkInID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return stored{}, false, nil
	}
	if err != nil {
		return stored{}, false, apperr.Wrap(err, "get check-in reply")
	}
	return stored{Reply: Reply{Kind: row.Kind, Text: row.Text}, ContentHash: row.ContentHash}, true, nil
}

// Put stores a check-in's note, replacing one written for older content.
func (r *Repository) Put(ctx context.Context, checkInID, userID uuid.UUID, hash string, reply Reply) error {
	err := r.q.UpsertReply(ctx, checkinreplydb.UpsertReplyParams{
		CheckInID: checkInID, UserID: userID, ContentHash: hash, Kind: reply.Kind, Text: reply.Text,
	})
	return apperr.Wrap(err, "store check-in reply")
}
