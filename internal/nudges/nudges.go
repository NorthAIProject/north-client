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
	KindGoalDeadline      = nudge.KindGoalDeadline
	KindFirstWeekCheck    = nudge.KindFirstWeekCheck
	KindFirstWeekEvidence = nudge.KindFirstWeekEvidence
	KindFirstWeekReview   = nudge.KindFirstWeekReview
	KindWorkoutToday      = nudge.KindWorkoutToday
	KindFormReady         = nudge.KindFormReady
	KindCoachReply        = nudge.KindCoachReply
	KindBriefingReady     = nudge.KindBriefingReady
	KindPhotoAsk          = nudge.KindPhotoAsk
	KindPhotoReminder     = nudge.KindPhotoReminder
)
