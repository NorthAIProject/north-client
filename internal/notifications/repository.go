package notifications

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	notificationsdb "github.com/NorthAIProject/north-client/internal/notifications/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *notificationsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: notificationsdb.New(pool)}
}

func (r *Repository) Get(ctx context.Context, userID uuid.UUID) (Prefs, error) {
	row, err := r.q.GetUserNotificationPrefs(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Prefs{}, apperr.ErrNotFound
		}
		return Prefs{}, apperr.Wrap(err, "get notification prefs")
	}
	return fromDB(row), nil
}

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, in Input) (Prefs, error) {
	row, err := r.q.UpsertUserNotificationPrefs(ctx, notificationsdb.UpsertUserNotificationPrefsParams{
		UserID:             userID,
		NudgeMissedCheckin: in.NudgeMissedCheckIn,
		NudgeGoalDeadline:  in.NudgeGoalDeadline,
		WeeklyReportAuto:   in.WeeklyReportAuto,
		DailyBriefingAuto:  in.DailyBriefingAuto,
		CoachActivity:      in.CoachActivity,
		TrainingReminders:  in.TrainingReminders,
		QuietHoursEnabled:  in.QuietHoursEnabled,
		QuietStart:         in.QuietStart,
		QuietEnd:           in.QuietEnd,
	})
	if err != nil {
		return Prefs{}, apperr.Wrap(err, "upsert notification prefs")
	}
	return fromDB(row), nil
}

func (r *Repository) GetSchedule(ctx context.Context, userID uuid.UUID, kind string) (Schedule, error) {
	row, err := r.q.GetAlertSchedule(ctx, notificationsdb.GetAlertScheduleParams{
		UserID: userID,
		Kind:   kind,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Schedule{}, apperr.ErrNotFound
		}
		return Schedule{}, apperr.Wrap(err, "get alert schedule")
	}
	return scheduleFromDB(row), nil
}

func (r *Repository) ListSchedules(ctx context.Context, userID uuid.UUID) ([]Schedule, error) {
	rows, err := r.q.ListAlertSchedules(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list alert schedules")
	}
	out := make([]Schedule, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduleFromDB(row))
	}
	return out, nil
}

func (r *Repository) UpsertSchedule(ctx context.Context, userID uuid.UUID, in ScheduleInput) (Schedule, error) {
	row, err := r.q.UpsertAlertSchedule(ctx, notificationsdb.UpsertAlertScheduleParams{
		UserID:       userID,
		Kind:         in.Kind,
		Enabled:      in.Enabled,
		EveryDays:    int32(in.EveryDays),
		ReminderDays: int32(in.ReminderDays),
	})
	if err != nil {
		return Schedule{}, apperr.Wrap(err, "upsert alert schedule")
	}
	return scheduleFromDB(row), nil
}
