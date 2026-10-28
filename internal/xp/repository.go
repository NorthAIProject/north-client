package xp

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	xpdb "github.com/NorthAIProject/north-client/internal/xp/db"
)

// PostgresRepository counts XP-paying actions and board participants in
// Postgres. It is the Repository the process wires into Service.
type PostgresRepository struct {
	q *xpdb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{q: xpdb.New(pool)}
}

// Earned counts each person's verified actions in a window, by kind.
func (r *PostgresRepository) Earned(ctx context.Context, ids []uuid.UUID, w window) (map[uuid.UUID]map[string]int, error) {
	rows, err := r.q.Earned(ctx, xpdb.EarnedParams{
		UserIds: ids, FromDay: w.fromDay, ToDay: w.toDay, FromAt: w.fromAt, ToAt: w.toAt,
		StreakFrom: StreakDayFrom, WorkoutMinSeconds: WorkoutMinMinutes * 60, WorkoutsPerDay: WorkoutsPaidPerDay,
		StreakMarks: streakMarks(), ChallengeMetKind: achievement.KindCrewChallengeMet,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "earned")
	}
	out := make(map[uuid.UUID]map[string]int, len(ids))
	for _, row := range rows {
		if out[row.UserID] == nil {
			out[row.UserID] = map[string]int{}
		}
		out[row.UserID][row.Kind] = int(row.N)
	}
	return out, nil
}

func (r *PostgresRepository) Participants(ctx context.Context, viewer uuid.UUID, category string) ([]xpdb.ParticipantsRow, error) {
	people, err := r.q.Participants(ctx, xpdb.ParticipantsParams{Viewer: viewer, Category: category})
	if err != nil {
		return nil, apperr.Wrap(err, "participants")
	}
	return people, nil
}

func (r *PostgresRepository) Shares(ctx context.Context, userID uuid.UUID, category string) (bool, error) {
	sharing, err := r.q.Shares(ctx, xpdb.SharesParams{UserID: userID, Category: category})
	if err != nil {
		return false, apperr.Wrap(err, "sharing")
	}
	return sharing, nil
}

// WorkoutsFinished is how many workouts each person finished between two
// instants. People with none are absent.
func (r *PostgresRepository) WorkoutsFinished(ctx context.Context, ids []uuid.UUID, fromAt, toAt time.Time) (map[uuid.UUID]int, error) {
	rows, err := r.q.WorkoutsFinished(ctx, xpdb.WorkoutsFinishedParams{UserIds: ids, FromAt: fromAt, ToAt: toAt})
	if err != nil {
		return nil, apperr.Wrap(err, "workouts")
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		out[row.UserID] = int(row.N)
	}
	return out, nil
}
