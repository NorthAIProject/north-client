package habits

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	habitsdb "github.com/NorthAIProject/north-client/internal/habits/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *habitsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: habitsdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, name, domain string, days []time.Weekday) (Habit, error) {
	row, err := r.q.CreateHabit(ctx, habitsdb.CreateHabitParams{
		UserID:     userID,
		Name:       name,
		Domain:     domain,
		DaysOfWeek: toWeekdayInts(days),
	})
	if err != nil {
		return Habit{}, apperr.Wrap(err, "create habit")
	}
	return fromDB(row), nil
}

func (r *Repository) Update(ctx context.Context, id, userID uuid.UUID, name, domain string, days []time.Weekday, active bool) (Habit, error) {
	row, err := r.q.UpdateHabit(ctx, habitsdb.UpdateHabitParams{
		ID:         id,
		UserID:     userID,
		Name:       name,
		Domain:     domain,
		DaysOfWeek: toWeekdayInts(days),
		Active:     active,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Habit{}, apperr.ErrNotFound
		}
		return Habit{}, apperr.Wrap(err, "update habit")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Habit, error) {
	row, err := r.q.GetHabit(ctx, habitsdb.GetHabitParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Habit{}, apperr.ErrNotFound
		}
		return Habit{}, apperr.Wrap(err, "get habit")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, activeOnly bool) ([]Habit, error) {
	rows, err := r.q.ListHabits(ctx, habitsdb.ListHabitsParams{UserID: userID, ActiveOnly: activeOnly})
	if err != nil {
		return nil, apperr.Wrap(err, "list habits")
	}

	out := make([]Habit, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	if err := r.q.DeleteHabit(ctx, habitsdb.DeleteHabitParams{ID: id, UserID: userID}); err != nil {
		return apperr.Wrap(err, "delete habit")
	}
	return nil
}

// Complete is idempotent: the query does nothing when the day is already
// ticked, so a double tap is not an error.
func (r *Repository) Complete(ctx context.Context, habitID, userID uuid.UUID, date time.Time) error {
	err := r.q.CompleteHabit(ctx, habitsdb.CompleteHabitParams{
		HabitID:   habitID,
		UserID:    userID,
		LocalDate: toDate(date),
	})
	if err != nil {
		return apperr.Wrap(err, "complete habit")
	}
	return nil
}

func (r *Repository) Uncomplete(ctx context.Context, habitID, userID uuid.UUID, date time.Time) error {
	err := r.q.UncompleteHabit(ctx, habitsdb.UncompleteHabitParams{
		HabitID:   habitID,
		UserID:    userID,
		LocalDate: toDate(date),
	})
	if err != nil {
		return apperr.Wrap(err, "uncomplete habit")
	}
	return nil
}

// CompletionsSince returns every completed date per habit, in one query
// rather than one per habit: a page showing eight habits should not make
// eight round trips to draw eight streaks.
func (r *Repository) CompletionsSince(ctx context.Context, userID uuid.UUID, since time.Time) (map[uuid.UUID][]time.Time, error) {
	rows, err := r.q.ListCompletionDatesSince(ctx, habitsdb.ListCompletionDatesSinceParams{
		UserID:    userID,
		LocalDate: toDate(since),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list habit completions")
	}

	out := make(map[uuid.UUID][]time.Time)
	for _, row := range rows {
		out[row.HabitID] = append(out[row.HabitID], fromDate(row.LocalDate))
	}
	return out, nil
}

func fromDB(row habitsdb.Habit) Habit {
	return Habit{
		ID:        row.ID,
		UserID:    row.UserID,
		Name:      row.Name,
		Domain:    row.Domain,
		Days:      fromWeekdayInts(row.DaysOfWeek),
		Active:    row.Active,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func toWeekdayInts(days []time.Weekday) []int16 {
	out := make([]int16, len(days))
	for i, d := range days {
		out[i] = int16(d)
	}
	return out
}

func fromWeekdayInts(in []int16) []time.Weekday {
	out := make([]time.Weekday, len(in))
	for i, d := range in {
		out[i] = time.Weekday(d)
	}
	return out
}

// Date conversion is duplicated per slice rather than shared, same as
// localMidnight, so feature slices stay independent.
func toDate(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func fromDate(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return d.Time
}
