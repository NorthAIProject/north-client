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
		QuietHoursEnabled:  in.QuietHoursEnabled,
		QuietStart:         in.QuietStart,
		QuietEnd:           in.QuietEnd,
	})
	if err != nil {
		return Prefs{}, apperr.Wrap(err, "upsert notification prefs")
	}
	return fromDB(row), nil
}
