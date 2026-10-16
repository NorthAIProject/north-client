package outreach

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	outreachdb "github.com/NorthAIProject/north-client/internal/outreach/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *outreachdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: outreachdb.New(pool)}
}

// Claim records today's outreach. claimed is false when one already exists
// for that day: the once-a-day limit.
func (r *Repository) Claim(ctx context.Context, userID uuid.UUID, day time.Time, trigger string, facts Facts) (Outreach, bool, error) {
	raw, err := json.Marshal(facts)
	if err != nil {
		return Outreach{}, false, apperr.Wrap(err, "encode outreach facts")
	}
	row, err := r.q.InsertOutreach(ctx, outreachdb.InsertOutreachParams{
		UserID: userID, Day: date(day), Trigger: trigger, Facts: raw,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Outreach{}, false, nil
	}
	if err != nil {
		return Outreach{}, false, apperr.Wrap(err, "claim outreach")
	}
	o, err := fromDB(row)
	return o, err == nil, err
}

// ForDay is the outreach on the person's local day, ErrNotFound when none.
func (r *Repository) ForDay(ctx context.Context, userID uuid.UUID, day time.Time) (Outreach, error) {
	row, err := r.q.GetOutreachForDay(ctx, outreachdb.GetOutreachForDayParams{UserID: userID, Day: date(day)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Outreach{}, apperr.ErrNotFound
	}
	if err != nil {
		return Outreach{}, apperr.Wrap(err, "load outreach")
	}
	return fromDB(row)
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Outreach, error) {
	row, err := r.q.GetOutreach(ctx, outreachdb.GetOutreachParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Outreach{}, apperr.ErrNotFound
	}
	if err != nil {
		return Outreach{}, apperr.Wrap(err, "load outreach")
	}
	return fromDB(row)
}

func (r *Repository) SetMessage(ctx context.Context, id, userID uuid.UUID, body string, messageID *uuid.UUID) error {
	if err := r.q.SetOutreachMessage(ctx, outreachdb.SetOutreachMessageParams{
		ID: id, UserID: userID, Body: body, MessageID: messageID,
	}); err != nil {
		return apperr.Wrap(err, "save outreach message")
	}
	return nil
}

// RecordAction stores the first answer. ErrConflict when one was already given.
func (r *Repository) RecordAction(ctx context.Context, id, userID uuid.UUID, action string) (Outreach, error) {
	row, err := r.q.RecordOutreachAction(ctx, outreachdb.RecordOutreachActionParams{ID: id, UserID: userID, Action: &action})
	if errors.Is(err, pgx.ErrNoRows) {
		return Outreach{}, apperr.Wrap(apperr.ErrConflict, "this was already answered")
	}
	if err != nil {
		return Outreach{}, apperr.Wrap(err, "record outreach answer")
	}
	return fromDB(row)
}

func fromDB(row outreachdb.CoachOutreach) (Outreach, error) {
	var facts Facts
	if len(row.Facts) > 0 {
		if err := json.Unmarshal(row.Facts, &facts); err != nil {
			return Outreach{}, apperr.Wrap(err, "decode outreach facts")
		}
	}
	o := Outreach{
		ID: row.ID, UserID: row.UserID, Day: row.Day.Time, Trigger: row.Trigger, Facts: facts,
		Body: row.Body, MessageID: row.MessageID, ActedAt: row.ActedAt, CreatedAt: row.CreatedAt,
	}
	if row.Action != nil {
		o.Action = *row.Action
	}
	return o, nil
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
