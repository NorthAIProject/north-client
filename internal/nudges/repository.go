package nudges

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	nudgesdb "github.com/NorthAIProject/north-client/internal/nudges/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *nudgesdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: nudgesdb.New(pool)}
}

// Draft is a nudge to insert if the dedupe key is new.
type Draft struct {
	Kind      string
	DedupeKey string
	Title     string
	Body      string
	Href      string
}

func (r *Repository) Insert(ctx context.Context, userID uuid.UUID, d Draft) (Nudge, bool, error) {
	row, err := r.q.InsertNudge(ctx, nudgesdb.InsertNudgeParams{
		UserID:    userID,
		Kind:      d.Kind,
		DedupeKey: d.DedupeKey,
		Title:     d.Title,
		Body:      d.Body,
		Href:      d.Href,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Nudge{}, false, nil
		}
		return Nudge{}, false, apperr.Wrap(err, "insert nudge")
	}
	return fromDB(row), true, nil
}

func (r *Repository) ListOpen(ctx context.Context, userID uuid.UUID, limit int) ([]Nudge, error) {
	rows, err := r.q.ListOpenNudges(ctx, nudgesdb.ListOpenNudgesParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list open nudges")
	}
	out := make([]Nudge, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *Repository) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountUnreadNudges(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count unread nudges")
	}
	return int(n), nil
}

func (r *Repository) MarkRead(ctx context.Context, id, userID uuid.UUID) (Nudge, error) {
	row, err := r.q.MarkNudgeRead(ctx, nudgesdb.MarkNudgeReadParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Nudge{}, apperr.ErrNotFound
		}
		return Nudge{}, apperr.Wrap(err, "mark nudge read")
	}
	return fromDB(row), nil
}

func (r *Repository) Dismiss(ctx context.Context, id, userID uuid.UUID) (Nudge, error) {
	row, err := r.q.DismissNudge(ctx, nudgesdb.DismissNudgeParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Nudge{}, apperr.ErrNotFound
		}
		return Nudge{}, apperr.Wrap(err, "dismiss nudge")
	}
	return fromDB(row), nil
}

func fromDB(row nudgesdb.UserNudge) Nudge {
	return Nudge{
		ID:          row.ID,
		UserID:      row.UserID,
		Kind:        row.Kind,
		DedupeKey:   row.DedupeKey,
		Title:       row.Title,
		Body:        row.Body,
		Href:        row.Href,
		ReadAt:      row.ReadAt,
		DismissedAt: row.DismissedAt,
		CreatedAt:   row.CreatedAt,
	}
}
