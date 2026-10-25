package awards

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	awardsdb "github.com/NorthAIProject/north-client/internal/awards/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// MonthlyMetric is what a month's challenge counts.
type MonthlyMetric string

const (
	MetricWorkouts    MonthlyMetric = "workouts"
	MetricHabitDays   MonthlyMetric = "habit_days"
	MetricCheckinDays MonthlyMetric = "checkin_days"
)

// The bar is the person's recent average plus 15%, never closer to the whole
// month than three days, and a fixed starter when there is nothing to average.
const (
	monthlyStretchPercent = 115
	monthlyMarginDays     = 3
	monthlyStarterWorkout = 4
	monthlyStarterDays    = 8
	monthlyWorkoutsAvg    = 4
)

// MonthlyChallenge is one local month's challenge with live progress.
type MonthlyChallenge struct {
	Month    time.Time // first day of the local month, in the person's zone
	Key      string    // "YYYY-MM"
	Metric   MonthlyMetric
	Target   int
	Progress int
	Met      bool
	MetAt    *time.Time
}

// MonthlyMedal is a met month, kept for good.
type MonthlyMedal struct {
	AchievementID uuid.UUID
	Key           string
	Metric        MonthlyMetric
	Target        int
	At            time.Time
}

// monthlyHistory is what the person did over the months that count: the (up
// to) three full local months before the challenge month in which the account
// existed. The totals are sums over Months months.
type monthlyHistory struct {
	Months      int
	Workouts    int
	HabitDays   int
	CheckinDays int
	ActiveHabit bool
}

// Having no history means no months, or months in which the chosen metric was
// never once logged: an empty past is a fresh start, not a target of 1.
//
// pickMonthly chooses the metric and target for the month starting monthStart,
// It is pure; createMonthly then lowers a days-metric target to what is still
// reachable (see reachableTarget).
func pickMonthly(h monthlyHistory, monthStart time.Time) (MonthlyMetric, int) {
	cap := daysIn(monthStart) - monthlyMarginDays
	switch {
	case h.Months > 0 && h.Workouts >= monthlyWorkoutsAvg*h.Months:
		return MetricWorkouts, clampTarget(stretch(h.Workouts, h.Months), cap)
	case h.ActiveHabit:
		t := monthlyStarterDays
		if h.HabitDays > 0 {
			t = stretch(h.HabitDays, h.Months)
		}
		return MetricHabitDays, clampTarget(t, cap)
	default:
		t := monthlyStarterDays
		if h.CheckinDays > 0 {
			t = stretch(h.CheckinDays, h.Months)
		}
		return MetricCheckinDays, clampTarget(t, cap)
	}
}

// reachableTarget lowers a days-metric target to what the month can still
// deliver: the days already logged plus the days left. It only ever lowers, and
// it wins over any floor, so the target stays reachable and never starts met.
func reachableTarget(target, done, daysLeft int) int {
	return max(1, min(target, done+daysLeft))
}

// stretch is ceil(total/months × 1.15), in integers so 4.0 never rounds up
// from float noise.
func stretch(total, months int) int {
	den := months * 100
	return (total*monthlyStretchPercent + den - 1) / den
}

// clampTarget keeps a target between 1 and hi.
func clampTarget(t, hi int) int {
	return max(1, min(t, hi))
}

// daysIn is the number of days in the month that starts at monthStart.
func daysIn(monthStart time.Time) int {
	return time.Date(monthStart.Year(), monthStart.Month()+1, 0, 12, 0, 0, 0, time.UTC).Day()
}

// monthStartOf is the first day of the local month containing t, in loc.
func monthStartOf(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, loc)
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

// monthKey is "YYYY-MM".
func monthKey(monthStart time.Time) string { return monthStart.Format("2006-01") }

func (s *Service) location(ctx context.Context, userID uuid.UUID) (string, *time.Location, error) {
	tz, err := s.q.Timezone(ctx, userID)
	if err != nil {
		return "", nil, fmt.Errorf("awards: timezone: %w", err)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "UTC", time.UTC, nil
	}
	return tz, loc, nil
}

// Monthly is the challenge of the local month containing now, created on first
// use from the person's recent months and fixed from then on, with live
// progress. Two first views racing create one row: the loser reads it.
func (s *Service) Monthly(ctx context.Context, userID uuid.UUID, now time.Time) (MonthlyChallenge, error) {
	tz, loc, err := s.location(ctx, userID)
	if err != nil {
		return MonthlyChallenge{}, err
	}
	start := monthStartOf(now, loc)
	row, err := s.q.GetMonthlyChallenge(ctx, awardsdb.GetMonthlyChallengeParams{UserID: userID, Month: pgDate(start)})
	if isNoRows(err) {
		if err = s.createMonthly(ctx, userID, tz, start, now.In(loc)); err != nil {
			return MonthlyChallenge{}, err
		}
		row, err = s.q.GetMonthlyChallenge(ctx, awardsdb.GetMonthlyChallengeParams{UserID: userID, Month: pgDate(start)})
	}
	if err != nil {
		return MonthlyChallenge{}, fmt.Errorf("awards: monthly challenge: %w", err)
	}
	c, err := s.liveMonthly(ctx, userID, tz, start, row)
	if err != nil {
		return MonthlyChallenge{}, err
	}
	// The owner path writes the medal as soon as the card would say "met", so
	// the card and the medal never disagree.
	if c.MetAt == nil && c.Progress >= c.Target {
		if err := s.recordMonthlyMet(ctx, userID, c.Key, now); err != nil {
			return MonthlyChallenge{}, err
		}
		c.MetAt = &now
	}
	return c, nil
}

// recordMonthlyMet writes the once-only met row; a repeat is a no-op.
func (s *Service) recordMonthlyMet(ctx context.Context, userID uuid.UUID, key string, now time.Time) error {
	_, err := s.q.InsertMonthlyMet(ctx, awardsdb.InsertMonthlyMetParams{
		UserID: userID, Title: i18n.Translate("en", "monthly.met"), OccurredAt: now, SourceKey: key,
	})
	if err != nil && !isNoRows(err) {
		return fmt.Errorf("awards: record monthly met: %w", err)
	}
	return nil
}

// PeekMonthly is Monthly without the lazy create: for someone looking at another
// person's page, which must never write their data or fix their target. ok is
// false when the month has no row yet.
func (s *Service) PeekMonthly(ctx context.Context, userID uuid.UUID, now time.Time) (MonthlyChallenge, bool, error) {
	tz, loc, err := s.location(ctx, userID)
	if err != nil {
		return MonthlyChallenge{}, false, err
	}
	start := monthStartOf(now, loc)
	row, err := s.q.GetMonthlyChallenge(ctx, awardsdb.GetMonthlyChallengeParams{UserID: userID, Month: pgDate(start)})
	if isNoRows(err) {
		return MonthlyChallenge{}, false, nil
	}
	if err != nil {
		return MonthlyChallenge{}, false, fmt.Errorf("awards: monthly challenge: %w", err)
	}
	c, err := s.liveMonthly(ctx, userID, tz, start, row)
	return c, err == nil, err
}

// liveMonthly adds live progress and the met state to a stored row.
func (s *Service) liveMonthly(ctx context.Context, userID uuid.UUID, tz string, start time.Time, row awardsdb.MonthlyChallenge) (MonthlyChallenge, error) {
	var err error
	c := MonthlyChallenge{Month: start, Key: monthKey(start), Metric: MonthlyMetric(row.Metric), Target: int(row.Target)}
	if c.Progress, err = s.monthlyProgress(ctx, userID, tz, start, c.Metric); err != nil {
		return MonthlyChallenge{}, err
	}
	metAt, err := s.q.GetMonthlyMetAt(ctx, awardsdb.GetMonthlyMetAtParams{UserID: userID, SourceKey: c.Key})
	switch {
	case err == nil:
		c.MetAt = &metAt
	case !isNoRows(err):
		return MonthlyChallenge{}, fmt.Errorf("awards: monthly met: %w", err)
	}
	c.Met = c.MetAt != nil || c.Progress >= c.Target
	return c, nil
}

func (s *Service) createMonthly(ctx context.Context, userID uuid.UUID, tz string, start, localNow time.Time) error {
	h, err := s.q.MonthlyHistory(ctx, awardsdb.MonthlyHistoryParams{UserID: userID, MonthStart: pgDate(start), Tz: tz})
	if err != nil {
		return fmt.Errorf("awards: monthly history: %w", err)
	}
	daysLeft := daysIn(start) - localNow.Day() + 1
	metric, target := pickMonthly(monthlyHistory{
		Months: int(h.Months), Workouts: int(h.Workouts), HabitDays: int(h.HabitDays),
		CheckinDays: int(h.CheckinDays), ActiveHabit: h.ActiveHabit,
	}, start)
	if metric != MetricWorkouts {
		// Days already logged this month count toward the target, so a row
		// created late must not start at or past it: that would hand out the
		// medal for work done before the challenge existed.
		done, err := s.monthlyProgress(ctx, userID, tz, start, metric)
		if err != nil {
			return err
		}
		target = reachableTarget(target, done, daysLeft)
	}
	if err := s.q.CreateMonthlyChallenge(ctx, awardsdb.CreateMonthlyChallengeParams{
		UserID: userID, Month: pgDate(start), Metric: string(metric), Target: int32(target),
	}); err != nil {
		return fmt.Errorf("awards: create monthly challenge: %w", err)
	}
	return nil
}

func (s *Service) monthlyProgress(ctx context.Context, userID uuid.UUID, tz string, start time.Time, m MonthlyMetric) (int, error) {
	from, to := pgDate(start), pgDate(start.AddDate(0, 1, 0))
	var n int32
	var err error
	switch m {
	case MetricWorkouts:
		n, err = s.q.CountMonthlyWorkouts(ctx, awardsdb.CountMonthlyWorkoutsParams{UserID: userID, Tz: tz, FromDay: from, ToDay: to})
	case MetricHabitDays:
		n, err = s.q.CountMonthlyHabitDays(ctx, awardsdb.CountMonthlyHabitDaysParams{UserID: userID, FromDay: from, ToDay: to})
	case MetricCheckinDays:
		n, err = s.q.CountMonthlyCheckinDays(ctx, awardsdb.CountMonthlyCheckinDaysParams{UserID: userID, FromDay: from, ToDay: to})
	default:
		return 0, fmt.Errorf("awards: unknown monthly metric %q", m)
	}
	if err != nil {
		return 0, fmt.Errorf("awards: monthly progress: %w", err)
	}
	return int(n), nil
}

// MonthlyMedals is every month the person met, newest first.
func (s *Service) MonthlyMedals(ctx context.Context, userID uuid.UUID) ([]MonthlyMedal, error) {
	rows, err := s.q.ListMonthlyMedals(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("awards: monthly medals: %w", err)
	}
	out := make([]MonthlyMedal, 0, len(rows))
	for _, r := range rows {
		out = append(out, MonthlyMedal{
			AchievementID: r.ID, Key: r.SourceKey, Metric: MonthlyMetric(r.Metric),
			Target: int(r.Target), At: r.OccurredAt,
		})
	}
	return out, nil
}

// UnseenMonthly is the met months not yet celebrated, oldest first.
func (s *Service) UnseenMonthly(ctx context.Context, userID uuid.UUID) ([]MonthlyMedal, error) {
	rows, err := s.q.ListUnseenMonthlyMedals(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("awards: unseen monthly medals: %w", err)
	}
	out := make([]MonthlyMedal, 0, len(rows))
	for _, r := range rows {
		out = append(out, MonthlyMedal{
			AchievementID: r.ID, Key: r.SourceKey, Metric: MonthlyMetric(r.Metric),
			Target: int(r.Target), At: r.OccurredAt,
		})
	}
	return out, nil
}

// holdsMonthly is ErrValidation unless the person holds the medal of month key.
func (s *Service) holdsMonthly(ctx context.Context, userID uuid.UUID, key string) error {
	if _, err := s.q.GetMonthlyMetAt(ctx, awardsdb.GetMonthlyMetAtParams{UserID: userID, SourceKey: key}); err != nil {
		if isNoRows(err) {
			return apperr.ErrValidation
		}
		return fmt.Errorf("awards: monthly held: %w", err)
	}
	return nil
}

// MarkMonthlySeen records that the person was shown the medal of month key. It
// is ErrNotFound unless they hold it; a repeat is fine.
func (s *Service) MarkMonthlySeen(ctx context.Context, userID uuid.UUID, key string) error {
	if _, err := s.q.GetMonthlyMetAt(ctx, awardsdb.GetMonthlyMetAtParams{UserID: userID, SourceKey: key}); err != nil {
		if isNoRows(err) {
			return apperr.ErrNotFound
		}
		return fmt.Errorf("awards: monthly held: %w", err)
	}
	if err := s.q.MarkMonthlySeen(ctx, awardsdb.MarkMonthlySeenParams{UserID: userID, SourceKey: key}); err != nil {
		return fmt.Errorf("awards: mark monthly seen: %w", err)
	}
	return nil
}

// checkMonthly records the current month as met once its target is reached:
// Monthly writes the medal itself, and skips the write once it exists. It only
// ever looks at the month of now, so an event backdated into an earlier month
// cannot grant that month. Best effort, like Check.
func (s *Service) checkMonthly(ctx context.Context, userID uuid.UUID, now time.Time) {
	if _, err := s.Monthly(ctx, userID, now); err != nil {
		s.log.Warn("could not check monthly challenge", "user_id", userID, "error", err)
	}
}

// monthlyAreas are the areas whose writes can move a monthly challenge.
func monthlyAreas(areas []award.Area) bool {
	if len(areas) == 0 {
		return true
	}
	for _, a := range areas {
		if a == award.Training || a == award.Habits || a == award.Consistency {
			return true
		}
	}
	return false
}
