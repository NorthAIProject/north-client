package inbox

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	inboxdb "github.com/NorthAIProject/north-client/internal/inbox/db"
	"github.com/NorthAIProject/north-client/internal/inbox/item"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// PostgresRepository keeps inbox items in Postgres. It is the Repository the
// process wires into Service.
type PostgresRepository struct {
	q *inboxdb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{q: inboxdb.New(pool)}
}

func (r *PostgresRepository) Add(ctx context.Context, userID uuid.UUID, source, text string) (Item, error) {
	row, err := r.q.AddItem(ctx, inboxdb.AddItemParams{UserID: userID, Source: source, Text: text})
	if err != nil {
		return Item{}, apperr.Wrap(err, "save inbox item")
	}
	return fromDB(row), nil
}

func (r *PostgresRepository) ListOpen(ctx context.Context, userID uuid.UUID, limit int) ([]Item, error) {
	rows, err := r.q.ListOpen(ctx, inboxdb.ListOpenParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list inbox")
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *PostgresRepository) CountOpen(ctx context.Context, userID uuid.UUID) (int, error) {
	count, err := r.q.CountOpen(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count inbox")
	}
	return int(count), nil
}

// Get is one of the person's items, or apperr.ErrNotFound.
func (r *PostgresRepository) Get(ctx context.Context, itemID, userID uuid.UUID) (Item, error) {
	row, err := r.q.GetItem(ctx, inboxdb.GetItemParams{ID: itemID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, apperr.ErrNotFound
	}
	if err != nil {
		return Item{}, apperr.Wrap(err, "load inbox item")
	}
	return fromDB(row), nil
}

func (r *PostgresRepository) SetSuggestion(ctx context.Context, itemID, userID uuid.UUID, sug Suggestion) error {
	raw, err := json.Marshal(sug)
	if err != nil {
		return apperr.Wrap(err, "encode inbox suggestion")
	}
	if err = r.q.SetSuggestion(ctx, inboxdb.SetSuggestionParams{ID: itemID, UserID: userID, Suggestion: raw}); err != nil {
		return apperr.Wrap(err, "save inbox suggestion")
	}
	return nil
}

// MarkFiled records where an item went; apperr.ErrNotFound when it is gone.
func (r *PostgresRepository) MarkFiled(ctx context.Context, itemID, userID uuid.UUID, destination string, ref uuid.UUID) error {
	n, err := r.q.MarkFiled(ctx, inboxdb.MarkFiledParams{ID: itemID, UserID: userID, FiledAs: &destination, FiledRef: &ref})
	if err != nil {
		return apperr.Wrap(err, "mark inbox item filed")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) Dismiss(ctx context.Context, itemID, userID uuid.UUID) error {
	n, err := r.q.Dismiss(ctx, inboxdb.DismissParams{ID: itemID, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "dismiss inbox item")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func fromDB(r inboxdb.InboxItem) Item {
	it := Item{ID: r.ID, UserID: r.UserID, Source: r.Source, Text: r.Text, Status: r.Status, CreatedAt: r.CreatedAt}
	if len(r.Suggestion) > 0 {
		var sug Suggestion
		if json.Unmarshal(r.Suggestion, &sug) == nil && item.ValidDestination(sug.Destination) {
			it.Suggestion = &sug
		}
	}
	return it
}
