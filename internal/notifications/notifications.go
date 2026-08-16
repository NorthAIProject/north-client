// Package notifications owns what North is allowed to say without being asked:
// which nudges may be raised, whether the weekly review writes itself, and the
// hours of the day the app stays quiet.
//
// It holds preferences only. Nothing here sends anything — internal/nudges and
// internal/reports ask this package whether they may act.
package notifications

import (
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	notificationsdb "github.com/NorthAIProject/north-client/internal/notifications/db"
)

// Prefs is one account's notification settings.
type Prefs struct {
	UserID uuid.UUID

	NudgeMissedCheckIn bool
	NudgeGoalDeadline  bool

	// WeeklyReportAuto opts in to the sweep that generates the weekly review
	// once the user's local week closes. Off unless asked for: each run is a
	// model call.
	WeeklyReportAuto bool

	QuietHoursEnabled bool
	// QuietStart and QuietEnd are "HH:MM" in the user's own timezone. The
	// window may wrap midnight, which is what makes InQuietHours worth having.
	QuietStart string
	QuietEnd   string

	UpdatedAt time.Time
}

// AllowsNudge reports whether a nudge of this kind may be raised. The kind is
// the string internal/nudges stores, kept as a plain string so that package
// does not have to import this one to name its own kinds.
func (p Prefs) AllowsNudge(kind string) bool {
	switch kind {
	case "missed_checkin":
		return p.NudgeMissedCheckIn
	case "goal_deadline":
		return p.NudgeGoalDeadline
	default:
		// An unknown kind is one this build does not offer a switch for.
		// Silence is not what somebody asked for, so allow it.
		return true
	}
}

// InQuietHours reports whether local falls inside the quiet window.
//
// local must already be in the user's timezone; this type has no opinion about
// which zone that is. A window whose end is at or before its start wraps
// midnight — 22:00 to 07:00 is the default, and is the whole reason this is a
// method rather than two comparisons at the call site.
func (p Prefs) InQuietHours(local time.Time) bool {
	if !p.QuietHoursEnabled {
		return false
	}

	start, okStart := parseHourMinute(p.QuietStart)
	end, okEnd := parseHourMinute(p.QuietEnd)
	if !okStart || !okEnd || start == end {
		// A malformed or empty window is no window. The column has a CHECK
		// constraint, so this only happens to a zero-value Prefs.
		return false
	}

	now := local.Hour()*60 + local.Minute()
	if start < end {
		return now >= start && now < end
	}
	// Wraps midnight: inside means after the start or before the end.
	return now >= start || now < end
}

// parseHourMinute reads "HH:MM" into minutes since midnight.
func parseHourMinute(s string) (int, bool) {
	hh, mm, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, false
	}
	hours, err := strconv.Atoi(hh)
	if err != nil || hours < 0 || hours > 23 {
		return 0, false
	}
	minutes, err := strconv.Atoi(mm)
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, false
	}
	return hours*60 + minutes, true
}

func fromDB(row notificationsdb.UserNotificationPref) Prefs {
	return Prefs{
		UserID:             row.UserID,
		NudgeMissedCheckIn: row.NudgeMissedCheckin,
		NudgeGoalDeadline:  row.NudgeGoalDeadline,
		WeeklyReportAuto:   row.WeeklyReportAuto,
		QuietHoursEnabled:  row.QuietHoursEnabled,
		QuietStart:         row.QuietStart,
		QuietEnd:           row.QuietEnd,
		UpdatedAt:          row.UpdatedAt,
	}
}
