package firstweek

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	firstweekdb "github.com/NorthAIProject/north-client/internal/firstweek/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	pool *pgxpool.Pool
	q    *firstweekdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, q: firstweekdb.New(pool)}
}

// Begin starts (or restarts) a person's week: the days of any earlier week
// are cleared and the programme row replaced, in one transaction.
func (r *Repository) Begin(ctx context.Context, p Programme) (Programme, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Programme{}, apperr.Wrap(err, "begin first week")
	}
	// Rollback after a successful Commit is a no-op, so this needs no flag.
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)
	if err = q.ClearDays(ctx, p.UserID); err != nil {
		return Programme{}, apperr.Wrap(err, "clear first week days")
	}
	row, err := q.UpsertProgramme(ctx, firstweekdb.UpsertProgrammeParams{
		UserID: p.UserID, GoalID: p.GoalID, Template: p.Template, Timezone: p.Timezone, StartedOn: date(p.StartedOn),
	})
	if err != nil {
		return Programme{}, apperr.Wrap(err, "save first week")
	}
	if err := tx.Commit(ctx); err != nil {
		return Programme{}, apperr.Wrap(err, "commit first week")
	}
	return fromDB(row), nil
}

// Get is the person's programme, ErrNotFound when they never had one.
func (r *Repository) Get(ctx context.Context, userID uuid.UUID) (Programme, error) {
	row, err := r.q.GetProgramme(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Programme{}, apperr.ErrNotFound
	}
	if err != nil {
		return Programme{}, apperr.Wrap(err, "load first week")
	}
	return fromDB(row), nil
}

// End moves a running week to status. changed is false when the week was not
// running, so a second skip or finish does nothing.
func (r *Repository) End(ctx context.Context, userID uuid.UUID, status string) (Programme, bool, error) {
	row, err := r.q.SetProgrammeStatus(ctx, firstweekdb.SetProgrammeStatusParams{UserID: userID, Status: status})
	if errors.Is(err, pgx.ErrNoRows) {
		return Programme{}, false, nil
	}
	if err != nil {
		return Programme{}, false, apperr.Wrap(err, "end first week")
	}
	return fromDB(row), true, nil
}

// ListActive pages the running weeks, by user id after `after`.
func (r *Repository) ListActive(ctx context.Context, after uuid.UUID, limit int) ([]Programme, error) {
	rows, err := r.q.ListActiveProgrammes(ctx, firstweekdb.ListActiveProgrammesParams{UserID: after, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list first weeks")
	}
	out := make([]Programme, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

// Complete records a day done. inserted is false when it already was.
func (r *Repository) Complete(ctx context.Context, userID uuid.UUID, day int, localDate time.Time, via string) (Completion, bool, error) {
	row, err := r.q.InsertDay(ctx, firstweekdb.InsertDayParams{
		UserID: userID, Day: int16(day), LocalDate: date(localDate), Via: via,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Completion{}, false, nil
	}
	if err != nil {
		return Completion{}, false, apperr.Wrap(err, "record first week day")
	}
	return completionFromDB(row), true, nil
}

// Completions are the days done so far, in order.
func (r *Repository) Completions(ctx context.Context, userID uuid.UUID) ([]Completion, error) {
	rows, err := r.q.ListDays(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list first week days")
	}
	out := make([]Completion, 0, len(rows))
	for _, row := range rows {
		out = append(out, completionFromDB(row))
	}
	return out, nil
}

// SetReview stores the week's review unless one is already there, and
// returns the programme with whichever review it now holds.
func (r *Repository) SetReview(ctx context.Context, userID uuid.UUID, review Review) (Programme, error) {
	body, err := json.Marshal(review)
	if err != nil {
		return Programme{}, apperr.Wrap(err, "encode first week review")
	}
	row, err := r.q.SetReview(ctx, firstweekdb.SetReviewParams{UserID: userID, Review: body})
	if errors.Is(err, pgx.ErrNoRows) {
		return r.Get(ctx, userID)
	}
	if err != nil {
		return Programme{}, apperr.Wrap(err, "save first week review")
	}
	return fromDB(row), nil
}

func fromDB(row firstweekdb.FirstWeekProgramme) Programme {
	p := Programme{
		UserID: row.UserID, GoalID: row.GoalID, Template: row.Template, Timezone: row.Timezone,
		StartedOn: row.StartedOn.Time, Status: row.Status, CreatedAt: row.CreatedAt, FinishedAt: row.FinishedAt,
	}
	if len(row.Review) > 0 {
		var review Review
		// A review that cannot be read is treated as not written rather
		// than failing every read of the week.
		if err := json.Unmarshal(row.Review, &review); err == nil {
			p.Review = &review
		}
	}
	return p
}

func completionFromDB(row firstweekdb.FirstWeekDay) Completion {
	return Completion{Day: int(row.Day), LocalDate: row.LocalDate.Time, Via: row.Via, CompletedAt: row.CompletedAt}
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
