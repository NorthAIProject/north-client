package checkins

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	checkinsdb "github.com/NorthAIProject/north-client/internal/checkins/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *checkinsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: checkinsdb.New(pool)}
}

// Write is a check-in to store. Every field is written as given.
type Write struct {
	LocalDate     time.Time
	Mood, Energy  int
	Wins          string
	Challenges    string
	Notes         string
	RelatedGoalID *uuid.UUID
	Stress        *int
	SleepQuality  *int
	Tags          []string
	// Source is recorded when the row is created and never changed after.
	Source checkin.Source
}

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, w Write) (CheckIn, error) {
	row, err := r.q.UpsertCheckIn(ctx, checkinsdb.UpsertCheckInParams{
		UserID:        userID,
		LocalDate:     toDate(w.LocalDate),
		Mood:          int16(w.Mood),
		Energy:        int16(w.Energy),
		Wins:          w.Wins,
		Challenges:    w.Challenges,
		Notes:         w.Notes,
		RelatedGoalID: w.RelatedGoalID,
		Source:        string(w.Source),
		Stress:        toInt16(w.Stress),
		SleepQuality:  toInt16(w.SleepQuality),
		Tags:          tagsOrEmpty(w.Tags),
	})
	if err != nil {
		return CheckIn{}, apperr.Wrap(err, "upsert check-in")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (CheckIn, error) {
	row, err := r.q.GetCheckIn(ctx, checkinsdb.GetCheckInParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CheckIn{}, apperr.ErrNotFound
		}
		return CheckIn{}, apperr.Wrap(err, "get check-in")
	}
	return fromDB(row), nil
}

func (r *Repository) GetByDate(ctx context.Context, userID uuid.UUID, localDate time.Time) (CheckIn, error) {
	row, err := r.q.GetCheckInByDate(ctx, checkinsdb.GetCheckInByDateParams{
		UserID:    userID,
		LocalDate: toDate(localDate),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CheckIn{}, apperr.ErrNotFound
		}
		return CheckIn{}, apperr.Wrap(err, "get check-in by date")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit int) ([]CheckIn, error) {
	rows, err := r.q.ListCheckIns(ctx, checkinsdb.ListCheckInsParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list check-ins")
	}
	return fromDBList(rows), nil
}

func (r *Repository) ListSince(ctx context.Context, userID uuid.UUID, since time.Time, limit int) ([]CheckIn, error) {
	rows, err := r.q.ListCheckInsSince(ctx, checkinsdb.ListCheckInsSinceParams{
		UserID:    userID,
		LocalDate: toDate(since),
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list check-ins since")
	}
	return fromDBList(rows), nil
}

// ListBetween returns the check-ins in the half-open window [since, until).
// Unlike ListSince there is no limit: the window is the limit.
func (r *Repository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]CheckIn, error) {
	rows, err := r.q.ListCheckInsBetween(ctx, checkinsdb.ListCheckInsBetweenParams{
		UserID:      userID,
		LocalDate:   toDate(since),
		LocalDate_2: toDate(until),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list check-ins between")
	}
	return fromDBList(rows), nil
}

func (r *Repository) Update(ctx context.Context, id, userID uuid.UUID, w Write) (CheckIn, error) {
	row, err := r.q.UpdateCheckIn(ctx, checkinsdb.UpdateCheckInParams{
		ID:            id,
		UserID:        userID,
		Mood:          int16(w.Mood),
		Energy:        int16(w.Energy),
		Wins:          w.Wins,
		Challenges:    w.Challenges,
		Notes:         w.Notes,
		RelatedGoalID: w.RelatedGoalID,
		Stress:        toInt16(w.Stress),
		SleepQuality:  toInt16(w.SleepQuality),
		Tags:          tagsOrEmpty(w.Tags),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CheckIn{}, apperr.ErrNotFound
		}
		return CheckIn{}, apperr.Wrap(err, "update check-in")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteCheckIn(ctx, checkinsdb.DeleteCheckInParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete check-in")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *Repository) Dates(ctx context.Context, userID uuid.UUID, limit int) ([]time.Time, error) {
	rows, err := r.q.ListCheckInDates(ctx, checkinsdb.ListCheckInDatesParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list check-in dates")
	}
	out := make([]time.Time, 0, len(rows))
	for _, d := range rows {
		if d.Valid {
			out = append(out, d.Time)
		}
	}
	return out, nil
}

func fromDBList(rows []checkinsdb.CheckIn) []CheckIn {
	out := make([]CheckIn, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out
}

func fromDB(row checkinsdb.CheckIn) CheckIn {
	c := CheckIn{
		ID:            row.ID,
		UserID:        row.UserID,
		Mood:          int(row.Mood),
		Energy:        int(row.Energy),
		Wins:          row.Wins,
		Challenges:    row.Challenges,
		Notes:         row.Notes,
		RelatedGoalID: row.RelatedGoalID,
		Stress:        fromInt16(row.Stress),
		SleepQuality:  fromInt16(row.SleepQuality),
		Tags:          tagsOrEmpty(row.Tags),
		Source:        checkin.Source(row.Source),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	if row.LocalDate.Valid {
		c.LocalDate = row.LocalDate.Time
	}
	return c
}

func toInt16(v *int) *int16 {
	if v == nil {
		return nil
	}
	n := int16(*v)
	return &n
}

func fromInt16(v *int16) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

// tagsOrEmpty keeps tags non-nil: the column is NOT NULL, and an empty JSON
// array reads better to clients than null.
func tagsOrEmpty(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func toDate(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	// Normalise to date-only in the location of t.
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return pgtype.Date{Time: d, Valid: true}
}

// Count is how many days this person has ever checked in.
func (r *Repository) Count(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountCheckIns(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count check-ins")
	}
	return int(n), nil
}

// Version is a cheap fingerprint of every check-in this person has: it changes
// on any save, edit or delete. The web's live displays poll it.
func (r *Repository) Version(ctx context.Context, userID uuid.UUID) (string, error) {
	row, err := r.q.CheckInVersion(ctx, userID)
	if err != nil {
		return "", apperr.Wrap(err, "check-in version")
	}
	return fmt.Sprintf("%d-%d", row.Latest.UnixMicro(), row.Total), nil
}
