package mind

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	minddb "github.com/NorthAIProject/north-client/internal/mind/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *minddb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: minddb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, content string, mood *int) (JournalEntry, error) {
	var moodParam *int16
	if mood != nil {
		v := int16(*mood)
		moodParam = &v
	}

	row, err := r.q.CreateJournalEntry(ctx, minddb.CreateJournalEntryParams{UserID: userID, Content: content, Mood: moodParam})
	if err != nil {
		return JournalEntry{}, apperr.Wrap(err, "create journal entry")
	}
	return fromDB(row), nil
}

func (r *Repository) Recent(ctx context.Context, userID uuid.UUID, limit int) ([]JournalEntry, error) {
	rows, err := r.q.ListJournalEntries(ctx, minddb.ListJournalEntriesParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list journal entries")
	}

	out := make([]JournalEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

// ListBetween returns the entries written in the half-open window
// [since, until), newest first.
func (r *Repository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]JournalEntry, error) {
	rows, err := r.q.ListJournalEntriesBetween(ctx, minddb.ListJournalEntriesBetweenParams{
		UserID:      userID,
		CreatedAt:   since,
		CreatedAt_2: until,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list journal entries between")
	}

	out := make([]JournalEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func fromDB(row minddb.JournalEntry) JournalEntry {
	e := JournalEntry{ID: row.ID, UserID: row.UserID, Content: row.Content, CreatedAt: row.CreatedAt}
	if row.Mood != nil {
		m := int(*row.Mood)
		e.Mood = &m
	}
	return e
}
