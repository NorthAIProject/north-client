package watches

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	watchesdb "github.com/NorthAIProject/north-client/internal/watches/db"
)

type Repository struct {
	q *watchesdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: watchesdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID, conversationID uuid.UUID, p Proposal, next time.Time) (Watch, error) {
	var convo *uuid.UUID
	if conversationID != uuid.Nil {
		convo = &conversationID
	}
	var weekday *int16
	if p.Schedule.Cadence == CadenceWeekly {
		d := int16(p.Schedule.Weekday)
		weekday = &d
	}

	row, err := r.q.CreateWatch(ctx, watchesdb.CreateWatchParams{
		UserID:         userID,
		ConversationID: convo,
		Title:          p.Title,
		Condition:      p.Condition,
		Spec:           p.Spec,
		Cadence:        string(p.Schedule.Cadence),
		Weekday:        weekday,
		MinuteOfDay:    int32(p.Schedule.Minute),
		NextRunAt:      next,
	})
	if err != nil {
		return Watch{}, apperr.Wrap(err, "create watch")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID) ([]Watch, error) {
	rows, err := r.q.ListWatches(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list watches")
	}
	return fromRows(rows), nil
}

// Due is every active watch whose slot has arrived, oldest first.
func (r *Repository) Due(ctx context.Context, now time.Time, limit int) ([]Watch, error) {
	rows, err := r.q.DueWatches(ctx, watchesdb.DueWatchesParams{NextRunAt: now, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list due watches")
	}
	return fromRows(rows), nil
}

// Claim moves a due watch to its next slot, reporting whether this caller is
// the one that did. dueAt is the slot the caller read; if another sweep has
// already moved it, nothing matches and the answer is false.
func (r *Repository) Claim(ctx context.Context, id uuid.UUID, dueAt, next, now time.Time) (bool, error) {
	n, err := r.q.ClaimWatchRun(ctx, watchesdb.ClaimWatchRunParams{
		ID:        id,
		DueAt:     dueAt,
		NextRunAt: next,
		RanAt:     &now,
	})
	if err != nil {
		return false, apperr.Wrap(err, "claim watch run")
	}
	return n == 1, nil
}

func fromRows(rows []watchesdb.CoachWatch) []Watch {
	out := make([]Watch, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out
}

func fromDB(row watchesdb.CoachWatch) Watch {
	w := Watch{
		ID:     row.ID,
		UserID: row.UserID,
		Proposal: Proposal{
			Title:     row.Title,
			Condition: row.Condition,
			Spec:      row.Spec,
			Schedule: Schedule{
				Cadence: Cadence(row.Cadence),
				Minute:  int(row.MinuteOfDay),
			},
		},
		Active:    row.Active,
		NextRunAt: row.NextRunAt,
		LastRunAt: row.LastRunAt,
		CreatedAt: row.CreatedAt,
	}
	if row.ConversationID != nil {
		w.ConversationID = *row.ConversationID
	}
	if row.Weekday != nil {
		w.Schedule.Weekday = time.Weekday(*row.Weekday)
	}
	return w
}
