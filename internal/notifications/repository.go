package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
		StatsDigestCadence: in.StatsDigestCadence,
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

// ClaimDigest records that a digest for this window is going out, and reports
// whether this caller is the one that gets to send it.
//
// The insert is the lock: two sweeps racing on the same window both try, one
// inserts, and the other is told it lost. Reading first and then writing would
// leave a gap between the two in which both could decide to send.
func (r *Repository) ClaimDigest(ctx context.Context, userID uuid.UUID, cadence string, periodStart time.Time) (bool, error) {
	_, err := r.q.ClaimStatsDigest(ctx, notificationsdb.ClaimStatsDigestParams{
		UserID:      userID,
		Cadence:     cadence,
		PeriodStart: pgtype.Date{Time: periodStart, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The conflict did nothing, so somebody has already sent this one.
		return false, nil
	}
	if err != nil {
		return false, apperr.Wrap(err, "claim stats digest")
	}
	return true, nil
}
