package meals

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func (r *Repository) CreateReminder(ctx context.Context, userID uuid.UUID, label, timeOfDay string, daysOfWeek []int) (Reminder, error) {
	row, err := r.q.CreateMealReminder(ctx, mealsdb.CreateMealReminderParams{
		UserID: userID, Label: label, TimeOfDay: timeOfDay, DaysOfWeek: intsToInt16s(daysOfWeek),
	})
	if err != nil {
		return Reminder{}, apperr.Wrap(err, "create meal reminder")
	}
	return reminderFromDB(row), nil
}

func (r *Repository) ListReminders(ctx context.Context, userID uuid.UUID) ([]Reminder, error) {
	rows, err := r.q.ListMealReminders(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list meal reminders")
	}
	out := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		out = append(out, reminderFromDB(row))
	}
	return out, nil
}

func (r *Repository) UpdateReminder(ctx context.Context, id, userID uuid.UUID, label, timeOfDay string, daysOfWeek []int) (Reminder, error) {
	row, err := r.q.UpdateMealReminder(ctx, mealsdb.UpdateMealReminderParams{
		ID: id, UserID: userID, Label: label, TimeOfDay: timeOfDay, DaysOfWeek: intsToInt16s(daysOfWeek),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Reminder{}, apperr.ErrNotFound
		}
		return Reminder{}, apperr.Wrap(err, "update meal reminder")
	}
	return reminderFromDB(row), nil
}

func (r *Repository) DeleteReminder(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteMealReminder(ctx, mealsdb.DeleteMealReminderParams{ID: id, UserID: userID}), "delete meal reminder")
}

func (r *Repository) SetReminderEnabled(ctx context.Context, id, userID uuid.UUID, enabled bool) (Reminder, error) {
	row, err := r.q.SetMealReminderEnabled(ctx, mealsdb.SetMealReminderEnabledParams{ID: id, UserID: userID, Enabled: enabled})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Reminder{}, apperr.ErrNotFound
		}
		return Reminder{}, apperr.Wrap(err, "set meal reminder enabled")
	}
	return reminderFromDB(row), nil
}

// NotYetFiredToday returns the user's enabled reminders that have not already
// been marked fired for asOfDate — day-of-week and time-of-day matching
// happens in Go via Reminder.DueOn.
func (r *Repository) NotYetFiredToday(ctx context.Context, userID uuid.UUID, asOfDate time.Time) ([]Reminder, error) {
	rows, err := r.q.ListNotYetFiredMealReminders(ctx, mealsdb.ListNotYetFiredMealRemindersParams{UserID: userID, AsOfDate: toDate(asOfDate)})
	if err != nil {
		return nil, apperr.Wrap(err, "list not yet fired meal reminders")
	}
	out := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		out = append(out, reminderFromDB(row))
	}
	return out, nil
}

func (r *Repository) MarkFired(ctx context.Context, id uuid.UUID, asOfDate time.Time) error {
	return apperr.Wrap(r.q.MarkMealReminderFired(ctx, mealsdb.MarkMealReminderFiredParams{ID: id, LastFiredLocalDate: toDate(asOfDate)}), "mark meal reminder fired")
}

func reminderFromDB(row mealsdb.MealReminder) Reminder {
	rem := Reminder{
		ID: row.ID, UserID: row.UserID, Label: row.Label, TimeOfDay: row.TimeOfDay,
		DaysOfWeek: int16sToInts(row.DaysOfWeek), Enabled: row.Enabled,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.LastFiredLocalDate.Valid {
		t := row.LastFiredLocalDate.Time
		rem.LastFiredLocalDate = &t
	}
	return rem
}
