package awards

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	awardsdb "github.com/NorthAIProject/north-client/internal/awards/db"
	"github.com/NorthAIProject/north-client/internal/hydration/hydration"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
)

// metric names what a badge counts. Badges that count the same thing (the
// four workout badges, the four streak badges) share one metric, so one
// evaluation runs each query once.
type metric string

const (
	mWorkouts         metric = "workouts"
	mEarlyBird        metric = "early_bird"
	mPRs              metric = "prs"
	mCentury          metric = "century"
	mCheckInRun       metric = "checkin_run"
	mHabitWeekRun     metric = "habit_week_run"
	mHabitCompletions metric = "habit_completions"
	mFoodRun          metric = "food_run"
	mProteinDays      metric = "protein_days"
	mWaterRun         metric = "water_run"
	mWaterDays        metric = "water_days"
	mRestedWeeks      metric = "rested_weeks"
	mSleepNights      metric = "sleep_nights"
	mJournal          metric = "journal"
	mMilestones       metric = "milestones"
	mGoalsOnTime      metric = "goals_on_time"
	mReviews          metric = "reviews"
	mCheckRun         metric = "check_run"
	mConnectedDays    metric = "connected_days"
	mFasts            metric = "fasts"
	mFollowers        metric = "followers"
	mCrew             metric = "crew"
)

// ruleOf is the rule for each badge. listened_to_body has none: it is earned
// only by Grant, when the lighter-day hook fires.
var ruleOf = map[string]metric{
	"workouts_1": mWorkouts, "workouts_10": mWorkouts, "workouts_50": mWorkouts, "workouts_100": mWorkouts,
	"early_bird": mEarlyBird,
	"pr_1":       mPRs, "pr_10": mPRs,
	"century":  mCentury,
	"streak_7": mCheckInRun, "streak_30": mCheckInRun, "streak_100": mCheckInRun, "streak_365": mCheckInRun,
	"habit_locked_in": mHabitWeekRun,
	"habit_100":       mHabitCompletions,
	"meals_week":      mFoodRun,
	"protein_10":      mProteinDays,
	"water_week":      mWaterRun,
	"water_30":        mWaterDays,
	"rested_week":     mRestedWeeks,
	"nights_30":       mSleepNights,
	"journal_1":       mJournal, "journal_10": mJournal,
	"milestone_1":    mMilestones,
	"goal_on_time":   mGoalsOnTime,
	"reviews_8":      mReviews,
	"checks_4":       mCheckRun,
	"connected_30":   mConnectedDays,
	"fasts_10":       mFasts,
	"first_follower": mFollowers,
	"crew_challenge": mCrew,
}

// counter computes metrics for one person, once each per evaluation.
type counter struct {
	ctx  context.Context
	q    *awardsdb.Queries
	user uuid.UUID
	tz   string
	tzOK bool
	seen map[metric]int
}

func newCounter(ctx context.Context, q *awardsdb.Queries, user uuid.UUID) *counter {
	return &counter{ctx: ctx, q: q, user: user, seen: map[metric]int{}}
}

func (c *counter) zone() (string, error) {
	if c.tzOK {
		return c.tz, nil
	}
	tz, err := c.q.Timezone(c.ctx, c.user)
	if err != nil {
		return "", fmt.Errorf("awards: timezone: %w", err)
	}
	c.tz, c.tzOK = tz, true
	return tz, nil
}

// count is the current figure for a metric.
func (c *counter) count(m metric) (int, error) {
	if n, ok := c.seen[m]; ok {
		return n, nil
	}
	n, err := c.compute(m)
	if err != nil {
		return 0, fmt.Errorf("awards: count %s: %w", m, err)
	}
	c.seen[m] = n
	return n, nil
}

func (c *counter) compute(m metric) (int, error) {
	ctx, q, u := c.ctx, c.q, c.user
	var n int32
	var err error
	switch m {
	case mWorkouts:
		n, err = q.CountWorkouts(ctx, u)
	case mEarlyBird:
		var tz string
		if tz, err = c.zone(); err == nil {
			n, err = q.CountEarlyBird(ctx, awardsdb.CountEarlyBirdParams{UserID: u, Tz: tz})
		}
	case mPRs:
		return c.personalRecords()
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
		var tz string
		if tz, err = c.zone(); err == nil {
			n, err = q.CountGoalsOnTime(ctx, awardsdb.CountGoalsOnTimeParams{UserID: u, Tz: tz})
		}
	case mReviews:
		n, err = q.CountWeeklyReviews(ctx, u)
	case mCheckRun:
		var gap int
		if gap, err = c.checkGap(); err == nil {
			n, err = q.LongestProgressCheckRun(ctx, awardsdb.LongestProgressCheckRunParams{UserID: u, MaxGap: int32(gap)})
		}
	case mConnectedDays:
		var tz string
		if tz, err = c.zone(); err == nil {
			n, err = q.CountConnectedDays(ctx, awardsdb.CountConnectedDaysParams{UserID: u, Tz: tz})
		}
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

// checkGap is the most days allowed between two progress checks for them to
// count as kept up: the current cadence plus three days of slack. With no
// schedule it is 31.
func (c *counter) checkGap() (int, error) {
	cad, err := c.q.ProgressCadence(c.ctx, c.user)
	if err != nil {
		if isNoRows(err) {
			return 31, nil
		}
		return 0, err
	}
	switch progresscheck.Cadence(cad) {
	case progresscheck.Weekly:
		return 7 + 3, nil
	case progresscheck.Biweekly:
		return 14 + 3, nil
	case progresscheck.Monthly:
		return 30 + 3, nil
	}
	return 31, nil
}

// personalRecords counts every record across all history. The first time an
// exercise is logged is a baseline, however much its sets ramp up within that
// session, so records set in an exercise's first session do not count (the
// same rule lift.Card applies to a workout's PR badge).
func (c *counter) personalRecords() (int, error) {
	rows, err := c.q.ListWorkSets(c.ctx, c.user)
	if err != nil {
		return 0, err
	}
	sets := make([]lift.Set, 0, len(rows))
	firstSession := map[string]string{}
	for _, r := range rows {
		s := lift.Set{
			ID: r.ID, ActivitySessionID: r.ActivitySessionID, LogDate: r.LogDate.Time,
			ExerciseSlug: r.ExerciseSlug, ExerciseName: r.ExerciseName, SetNumber: int(r.SetNumber),
			WeightKg: r.WeightKg, Reps: int(r.Reps), PerformedAt: r.PerformedAt, Kind: r.Kind,
		}
		sets = append(sets, s)
		if _, ok := firstSession[s.Key()]; !ok {
			firstSession[s.Key()] = sessionKey(s)
		}
	}
	n := 0
	for _, rec := range lift.Records(sets) {
		if sessionKey(rec.Set) != firstSession[rec.Set.Key()] {
			n++
		}
	}
	return n, nil
}

// sessionKey is the workout a set belongs to; sets outside a workout share
// their day.
func sessionKey(s lift.Set) string {
	if s.ActivitySessionID != nil {
		return s.ActivitySessionID.String()
	}
	return "day:" + s.LogDate.Format("2006-01-02")
}

// progressOf is the current count behind a badge, 0 for a badge with no rule.
func (c *counter) progressOf(b award.Badge) (int, error) {
	m, ok := ruleOf[b.ID]
	if !ok {
		return 0, nil
	}
	return c.count(m)
}
