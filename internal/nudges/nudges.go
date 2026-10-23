// Package nudges owns scheduled coach accountability: missed check-ins,
// approaching goal deadlines, training days, and the first-week notes. The
// worker evaluates the rules and delivers — to the bell, to a linked Telegram
// chat, and to subscribed browsers over Web Push; the web process lists, marks
// read, dismisses, and attributes an open to the channel that brought it.
package nudges

import "github.com/NorthAIProject/north-client/internal/nudges/nudge"

type Nudge = nudge.Nudge

const (
	KindMissedCheckIn     = nudge.KindMissedCheckIn
	KindStreakAtRisk      = nudge.KindStreakAtRisk
	KindGoalDeadline      = nudge.KindGoalDeadline
	KindFirstWeekCheck    = nudge.KindFirstWeekCheck
	KindFirstWeekEvidence = nudge.KindFirstWeekEvidence
	KindFirstWeekReview   = nudge.KindFirstWeekReview
	KindFirstWeekEvening  = nudge.KindFirstWeekEvening
	KindWorkoutToday      = nudge.KindWorkoutToday
	KindFormReady         = nudge.KindFormReady
	KindCoachReply        = nudge.KindCoachReply
	KindBriefingReady     = nudge.KindBriefingReady
	KindPhotoAsk          = nudge.KindPhotoAsk
	KindPhotoReminder     = nudge.KindPhotoReminder
	KindMealReminder      = nudge.KindMealReminder
	KindEveningReflection = nudge.KindEveningReflection
	KindCrewCheckIn       = nudge.KindCrewCheckIn
	KindWeekReview        = nudge.KindWeekReview
	KindDecisionRevisit   = nudge.KindDecisionRevisit

	KindMedicationReminder = nudge.KindMedicationReminder
	KindProgressCheckDue   = nudge.KindProgressCheckDue
	KindCoachLighter       = nudge.KindCoachLighter
	KindCoachMissed        = nudge.KindCoachMissed
	KindCoachFirstWeek     = nudge.KindCoachFirstWeek
)

// CategoryCheckIn is the iOS notification category that puts mood buttons
// under a banner. The app registers the same name; renaming it on one side
// silently drops the buttons.
const CategoryCheckIn = "CHECKIN"

// CategoryCoachLighter and CategoryCoachMissed put the coach's one-tap
// answers under its banner: Undo for a session it lightened, Today /
// Tomorrow / Let it go for a missed one. Same contract as CategoryCheckIn:
// the app registers these exact names.
//
// CategoryCoachFirstWeek puts Done under the first week's morning message
// and its evening note; the app answers it with POST /api/v1/first-week/done.
const (
	CategoryCoachLighter   = "COACH_LIGHTER"
	CategoryCoachMissed    = "COACH_MISSED"
	CategoryCoachFirstWeek = "COACH_FIRST_WEEK"
)

// PushCategory is the notification category a kind of nudge is sent with, or
// empty for a plain banner. Only the nudges that ask for a check-in get one.
func PushCategory(kind string) string {
	switch kind {
	case KindMissedCheckIn, KindStreakAtRisk, KindCrewCheckIn, KindEveningReflection:
		return CategoryCheckIn
	case KindCoachLighter:
		return CategoryCoachLighter
	case KindCoachMissed:
		return CategoryCoachMissed
	case KindCoachFirstWeek, KindFirstWeekEvening:
		return CategoryCoachFirstWeek
	default:
		return ""
	}
}
