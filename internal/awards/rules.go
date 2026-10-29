package awards

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
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
	repo Repository
	user uuid.UUID
	tz   string
	tzOK bool
	seen map[metric]int
	fail metric // test seam: this metric errors
}

func newCounter(ctx context.Context, repo Repository, user uuid.UUID) *counter {
	return &counter{ctx: ctx, repo: repo, user: user, seen: map[metric]int{}}
}

func (c *counter) zone() (string, error) {
	if c.tzOK {
		return c.tz, nil
	}
	tz, err := c.repo.Timezone(c.ctx, c.user)
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
	var n int
	var err error
	if c.fail != "" && c.fail == m {
		err = errors.New("injected failure")
	} else {
		n, err = c.compute(m)
	}
	if err != nil {
		return 0, fmt.Errorf("awards: count %s: %w", m, err)
	}
	c.seen[m] = n
	return n, nil
}

// compute reads a metric's figure. The zone and the progress-check gap are
// looked up only for the metrics that use them.
func (c *counter) compute(m metric) (int, error) {
	var tz string
	var gap int
	var err error
	switch m {
	case mPRs:
		return c.personalRecords()
	case mEarlyBird, mGoalsOnTime, mConnectedDays:
		tz, err = c.zone()
	case mCheckRun:
		gap, err = c.checkGap()
	}
	if err != nil {
		return 0, err
	}
	return c.repo.Count(c.ctx, c.user, m, tz, gap)
}

// checkGap is the most days allowed between two progress checks for them to
// count as kept up: the current cadence plus three days of slack. With no
// schedule it is 31.
func (c *counter) checkGap() (int, error) {
	cad, err := c.repo.ProgressCadence(c.ctx, c.user)
	if err != nil {
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
	sets, err := c.repo.WorkSets(c.ctx, c.user)
	if err != nil {
		return 0, err
	}
	firstSession := map[string]string{}
	for _, s := range sets {
		if _, ok := firstSession[s.Key()]; !ok {
			firstSession[s.Key()] = sessionKey(s)
		}
	}
	// Several record sets of one exercise in one session are one PR.
	prs := map[string]bool{}
	for _, rec := range lift.Records(sets) {
		if sk := sessionKey(rec.Set); sk != firstSession[rec.Set.Key()] {
			prs[rec.Set.Key()+"|"+sk] = true
		}
	}
	return len(prs), nil
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
