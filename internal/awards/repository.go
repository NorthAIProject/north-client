package awards

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	awardsdb "github.com/NorthAIProject/north-client/internal/awards/db"
	"github.com/NorthAIProject/north-client/internal/hydration/hydration"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// PostgresRepository keeps badges, monthly challenges and badge rarity in
// Postgres, and counts the history the badge rules read. It is the
// Repository the process wires into Service.
type PostgresRepository struct {
	q *awardsdb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{q: awardsdb.New(pool)}
}

// heldBadge is one badge row a person holds.
type heldBadge struct {
	AchievementID uuid.UUID
	BadgeID       string
	At            time.Time
	Celebrated    bool
}

// storedMonthly is a month's challenge as it was fixed on first use.
type storedMonthly struct {
	Metric MonthlyMetric
	Target int
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

func (r *PostgresRepository) Timezone(ctx context.Context, userID uuid.UUID) (string, error) {
	return r.q.Timezone(ctx, userID)
}

// Count is the current figure for a counted metric. tz is used by the
// metrics counted in local days, maxGap by the progress-check run.
func (r *PostgresRepository) Count(ctx context.Context, u uuid.UUID, m metric, tz string, maxGap int) (int, error) {
	q := r.q
	var n int32
	var err error
	switch m {
	case mWorkouts:
		n, err = q.CountWorkouts(ctx, u)
	case mEarlyBird:
		n, err = q.CountEarlyBird(ctx, awardsdb.CountEarlyBirdParams{UserID: u, Tz: tz})
	case mCentury:
		n, err = q.CountCentury(ctx, u)
	case mCheckInRun:
		n, err = q.LongestCheckInRun(ctx, u)
	case mHabitWeekRun:
		n, err = q.LongestHabitWeekRun(ctx, u)
	case mHabitCompletions:
		n, err = q.CountHabitCompletions(ctx, u)
	case mFoodRun:
		n, err = q.LongestFoodLogRun(ctx, u)
	case mProteinDays:
		n, err = q.CountProteinDays(ctx, u)
	case mWaterRun:
		n, err = q.LongestWaterRun(ctx, awardsdb.LongestWaterRunParams{UserID: u, TargetMl: hydration.DefaultDailyTargetML})
	case mWaterDays:
		n, err = q.CountWaterDays(ctx, awardsdb.CountWaterDaysParams{UserID: u, TargetMl: hydration.DefaultDailyTargetML})
	case mRestedWeeks:
		n, err = q.CountRestedWeeks(ctx, u)
	case mSleepNights:
		n, err = q.CountSleepNights(ctx, u)
	case mJournal:
		n, err = q.CountJournalEntries(ctx, u)
	case mMilestones:
		n, err = q.CountMilestones(ctx, u)
	case mGoalsOnTime:
		n, err = q.CountGoalsOnTime(ctx, awardsdb.CountGoalsOnTimeParams{UserID: u, Tz: tz})
	case mReviews:
		n, err = q.CountWeeklyReviews(ctx, u)
	case mCheckRun:
		n, err = q.LongestProgressCheckRun(ctx, awardsdb.LongestProgressCheckRunParams{UserID: u, MaxGap: int32(maxGap)})
	case mConnectedDays:
		n, err = q.CountConnectedDays(ctx, awardsdb.CountConnectedDaysParams{UserID: u, Tz: tz})
	case mFasts:
		n, err = q.CountFasts(ctx, u)
	case mFollowers:
		n, err = q.CountFollowers(ctx, u)
	case mCrew:
		n, err = q.CountCrewChallenges(ctx, u)
	default:
		return 0, fmt.Errorf("no rule for metric %q", m)
	}
	return int(n), err
}

// ProgressCadence is the person's progress-check cadence, empty with no
// schedule.
func (r *PostgresRepository) ProgressCadence(ctx context.Context, userID uuid.UUID) (string, error) {
	cad, err := r.q.ProgressCadence(ctx, userID)
	if err != nil {
		if isNoRows(err) {
			return "", nil
		}
		return "", err
	}
	return cad, nil
}

// WorkSets is every logged work set across the person's history, in the
// order the record walk needs.
func (r *PostgresRepository) WorkSets(ctx context.Context, userID uuid.UUID) ([]lift.Set, error) {
	rows, err := r.q.ListWorkSets(ctx, userID)
	if err != nil {
		return nil, err
	}
	sets := make([]lift.Set, 0, len(rows))
	for _, row := range rows {
		sets = append(sets, lift.Set{
			ID: row.ID, ActivitySessionID: row.ActivitySessionID, LogDate: row.LogDate.Time,
			ExerciseSlug: row.ExerciseSlug, ExerciseName: row.ExerciseName, SetNumber: int(row.SetNumber),
			WeightKg: row.WeightKg, Reps: int(row.Reps), PerformedAt: row.PerformedAt, Kind: row.Kind,
		})
	}
	return sets, nil
}

// Badges is every badge row the person holds, oldest first.
func (r *PostgresRepository) Badges(ctx context.Context, userID uuid.UUID) ([]heldBadge, error) {
	rows, err := r.q.ListBadges(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]heldBadge, 0, len(rows))
	for _, row := range rows {
		out = append(out, heldBadge{
			AchievementID: row.ID, BadgeID: row.SourceKey, At: row.OccurredAt, Celebrated: row.CelebratedAt != nil,
		})
	}
	return out, nil
}

// InsertBadge stores a badge once. inserted is false when the person already
// had it.
func (r *PostgresRepository) InsertBadge(ctx context.Context, userID uuid.UUID, b award.Badge, at time.Time) (uuid.UUID, bool, error) {
	id, err := r.q.InsertBadge(ctx, awardsdb.InsertBadgeParams{
		UserID: userID, Title: i18n.Translate("en", "awards.badge."+b.ID+".name"),
		OccurredAt: at, SourceKey: b.ID, Private: b.Area.Private(),
	})
	if err != nil {
		if isNoRows(err) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return id, true, nil
}

func (r *PostgresRepository) MarkBadgeSeen(ctx context.Context, userID uuid.UUID, badgeID string) error {
	return r.q.MarkBadgeSeen(ctx, awardsdb.MarkBadgeSeenParams{UserID: userID, SourceKey: badgeID})
}

// ActiveAreas is the badge areas the person logged in since since.
func (r *PostgresRepository) ActiveAreas(ctx context.Context, userID uuid.UUID, since time.Time) ([]award.Area, error) {
	rows, err := r.q.ActiveAreas(ctx, awardsdb.ActiveAreasParams{UserID: userID, Since: since})
	if err != nil {
		return nil, err
	}
	areas := make([]award.Area, len(rows))
	for i, row := range rows {
		areas[i] = award.Area(row)
	}
	return areas, nil
}

// MonthlyChallenge is the stored challenge of the month starting at start,
// or apperr.ErrNotFound.
func (r *PostgresRepository) MonthlyChallenge(ctx context.Context, userID uuid.UUID, start time.Time) (storedMonthly, error) {
	row, err := r.q.GetMonthlyChallenge(ctx, awardsdb.GetMonthlyChallengeParams{UserID: userID, Month: pgDate(start)})
	if isNoRows(err) {
		return storedMonthly{}, apperr.ErrNotFound
	}
	if err != nil {
		return storedMonthly{}, err
	}
	return storedMonthly{Metric: MonthlyMetric(row.Metric), Target: int(row.Target)}, nil
}

func (r *PostgresRepository) CreateMonthlyChallenge(ctx context.Context, userID uuid.UUID, start time.Time, m MonthlyMetric, target int) error {
	return r.q.CreateMonthlyChallenge(ctx, awardsdb.CreateMonthlyChallengeParams{
		UserID: userID, Month: pgDate(start), Metric: string(m), Target: int32(target),
	})
}

// MonthlyHistory is what the person did over the months before start that
// count toward picking its challenge.
func (r *PostgresRepository) MonthlyHistory(ctx context.Context, userID uuid.UUID, start time.Time, tz string) (monthlyHistory, error) {
	h, err := r.q.MonthlyHistory(ctx, awardsdb.MonthlyHistoryParams{UserID: userID, MonthStart: pgDate(start), Tz: tz})
	if err != nil {
		return monthlyHistory{}, err
	}
	return monthlyHistory{
		Months: int(h.Months), Workouts: int(h.Workouts), HabitDays: int(h.HabitDays),
		CheckinDays: int(h.CheckinDays), ActiveHabit: h.ActiveHabit,
	}, nil
}

// MonthlyProgress is the figure of metric m in the month starting at start.
func (r *PostgresRepository) MonthlyProgress(ctx context.Context, userID uuid.UUID, tz string, start time.Time, m MonthlyMetric) (int, error) {
	from, to := pgDate(start), pgDate(start.AddDate(0, 1, 0))
	var n int32
	var err error
	switch m {
	case MetricWorkouts:
		n, err = r.q.CountMonthlyWorkouts(ctx, awardsdb.CountMonthlyWorkoutsParams{UserID: userID, Tz: tz, FromDay: from, ToDay: to})
	case MetricHabitDays:
		n, err = r.q.CountMonthlyHabitDays(ctx, awardsdb.CountMonthlyHabitDaysParams{UserID: userID, FromDay: from, ToDay: to})
	case MetricCheckinDays:
		n, err = r.q.CountMonthlyCheckinDays(ctx, awardsdb.CountMonthlyCheckinDaysParams{UserID: userID, FromDay: from, ToDay: to})
	default:
		return 0, fmt.Errorf("awards: unknown monthly metric %q", m)
	}
	if err != nil {
		return 0, fmt.Errorf("awards: monthly progress: %w", err)
	}
	return int(n), nil
}

// MonthlyMetAt is when the medal of month key was won, or apperr.ErrNotFound.
func (r *PostgresRepository) MonthlyMetAt(ctx context.Context, userID uuid.UUID, key string) (time.Time, error) {
	at, err := r.q.GetMonthlyMetAt(ctx, awardsdb.GetMonthlyMetAtParams{UserID: userID, SourceKey: key})
	if isNoRows(err) {
		return time.Time{}, apperr.ErrNotFound
	}
	return at, err
}

// InsertMonthlyMet writes the once-only met row; a repeat is a no-op.
func (r *PostgresRepository) InsertMonthlyMet(ctx context.Context, userID uuid.UUID, key string, at time.Time) error {
	_, err := r.q.InsertMonthlyMet(ctx, awardsdb.InsertMonthlyMetParams{
		UserID: userID, Title: i18n.Translate("en", "monthly.met"), OccurredAt: at, SourceKey: key,
	})
	if err != nil && !isNoRows(err) {
		return err
	}
	return nil
}

func (r *PostgresRepository) MarkMonthlySeen(ctx context.Context, userID uuid.UUID, key string) error {
	return r.q.MarkMonthlySeen(ctx, awardsdb.MarkMonthlySeenParams{UserID: userID, SourceKey: key})
}

// MonthlyMedals is every month the person met, newest first.
func (r *PostgresRepository) MonthlyMedals(ctx context.Context, userID uuid.UUID) ([]MonthlyMedal, error) {
	rows, err := r.q.ListMonthlyMedals(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("awards: monthly medals: %w", err)
	}
	out := make([]MonthlyMedal, 0, len(rows))
	for _, row := range rows {
		out = append(out, MonthlyMedal{
			AchievementID: row.ID, Key: row.SourceKey, Metric: MonthlyMetric(row.Metric),
			Target: int(row.Target), At: row.OccurredAt,
		})
	}
	return out, nil
}

// UnseenMonthlyMedals is the met months not yet celebrated, oldest first.
func (r *PostgresRepository) UnseenMonthlyMedals(ctx context.Context, userID uuid.UUID) ([]MonthlyMedal, error) {
	rows, err := r.q.ListUnseenMonthlyMedals(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("awards: unseen monthly medals: %w", err)
	}
	out := make([]MonthlyMedal, 0, len(rows))
	for _, row := range rows {
		out = append(out, MonthlyMedal{
			AchievementID: row.ID, Key: row.SourceKey, Metric: MonthlyMetric(row.Metric),
			Target: int(row.Target), At: row.OccurredAt,
		})
	}
	return out, nil
}

// Rarities is the last computed rarity of every badge that has one.
func (r *PostgresRepository) Rarities(ctx context.Context) (map[string]Rarity, error) {
	rows, err := r.q.ListRarity(ctx)
	if err != nil {
		return nil, fmt.Errorf("awards: list rarity: %w", err)
	}
	out := make(map[string]Rarity, len(rows))
	for _, row := range rows {
		out[row.BadgeID] = Rarity{BadgeID: row.BadgeID, Earned: int(row.Earned), Active: int(row.Active)}
	}
	return out, nil
}

// CountActiveUsers is how many people logged anything since since.
func (r *PostgresRepository) CountActiveUsers(ctx context.Context, since time.Time) (int, error) {
	n, err := r.q.CountActiveUsers(ctx, since)
	return int(n), err
}

// CountBadgeEarners is how many people hold each badge; a badge nobody holds
// is absent.
func (r *PostgresRepository) CountBadgeEarners(ctx context.Context) (map[string]int, error) {
	rows, err := r.q.CountBadgeEarners(ctx)
	if err != nil {
		return nil, err
	}
	earned := make(map[string]int, len(rows))
	for _, row := range rows {
		earned[row.SourceKey] = int(row.Earned)
	}
	return earned, nil
}

func (r *PostgresRepository) UpsertRarity(ctx context.Context, rarity Rarity, at time.Time) error {
	return r.q.UpsertRarity(ctx, awardsdb.UpsertRarityParams{
		BadgeID: rarity.BadgeID, Earned: int32(rarity.Earned), Active: int32(rarity.Active), ComputedAt: at,
	})
}
