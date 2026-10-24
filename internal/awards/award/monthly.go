package award

import (
	"context"
	"regexp"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// MonthlyArea is the area of a monthly medal on the wire. It is not a catalog
// area: a monthly medal is not a badge.
const MonthlyArea = "monthly"

const monthlyPrefix = "monthly-"

var monthlyKeyRE = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// MonthlyID is the wire id of the medal for a month key such as "2026-10".
func MonthlyID(key string) string { return monthlyPrefix + key }

// ParseMonthlyID is the month key in a wire id such as "monthly-2026-10".
// ok is false for anything else, including a malformed month.
func ParseMonthlyID(id string) (key string, ok bool) {
	if len(id) <= len(monthlyPrefix) || id[:len(monthlyPrefix)] != monthlyPrefix {
		return "", false
	}
	key = id[len(monthlyPrefix):]
	return key, monthlyKeyRE.MatchString(key)
}

// MonthlyMedalView says which month and metric a monthly medal is for, so a
// client can draw the right coin without reading the localised text.
type MonthlyMedalView struct {
	Month  string `json:"month"`
	Metric string `json:"metric"`
	Target int    `json:"target"`
}

// MonthlyChallengeView is the current month's challenge, worded in the
// request's locale.
type MonthlyChallengeView struct {
	Month        string `json:"month"`
	Metric       string `json:"metric"`
	Target       int    `json:"target"`
	Progress     int    `json:"progress"`
	Met          bool   `json:"met"`
	Title        string `json:"title"`
	Goal         string `json:"goal"`
	ProgressText string `json:"progressText"`
}

// monthOf is the first day of the month key; the zero time for a bad key.
func monthOf(key string) time.Time {
	t, err := time.Parse("2006-01", key)
	if err != nil {
		return time.Time{}
	}
	return t
}

// MonthlyMedalName is "October 2026 challenge" in the request's locale.
func MonthlyMedalName(ctx context.Context, key string) string {
	return i18n.Tf(ctx, "monthly.medal.name", i18n.Date(ctx, monthOf(key), "January 2006"))
}

// MonthlyMedalDescription is what the month's medal was for, such as "Logged 12
// workouts in October".
func MonthlyMedalDescription(ctx context.Context, key, metric string, target int) string {
	return i18n.Tf(ctx, "monthly.medal.description."+metric, target, i18n.Date(ctx, monthOf(key), "January"))
}

// MonthlyMedal is a met month as a View: an award whose id is monthly-YYYY-MM.
func MonthlyMedal(ctx context.Context, key, metric string, target int, at time.Time) View {
	return View{
		ID: MonthlyID(key), Area: MonthlyArea,
		Name: MonthlyMedalName(ctx, key), Description: MonthlyMedalDescription(ctx, key, metric, target),
		EarnedAt: &at, Target: target,
		Monthly: &MonthlyMedalView{Month: key, Metric: metric, Target: target},
	}
}

// MonthlyChallenge is the current month's challenge as a view. Progress is
// capped at the target so nothing reads "12 of 8".
func MonthlyChallenge(ctx context.Context, key, metric string, target, progress int, met bool) MonthlyChallengeView {
	progress = min(progress, target)
	month := i18n.Date(ctx, monthOf(key), "January")
	return MonthlyChallengeView{
		Month: key, Metric: metric, Target: target, Progress: progress, Met: met,
		Title:        i18n.Tf(ctx, "monthly.title", month),
		Goal:         i18n.Tf(ctx, "monthly.goal."+metric, target, month),
		ProgressText: i18n.Tf(ctx, "monthly.progress."+metric, progress, target),
	}
}
